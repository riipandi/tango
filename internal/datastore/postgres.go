package datastore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Pool defaults, applied when the matching option is zero.
const (
	defaultMaxConns          = 10
	defaultMinConns          = 2
	defaultMaxConnLifetime   = time.Hour
	defaultMaxConnIdleTime   = 30 * time.Minute
	defaultHealthCheckPeriod = time.Minute
	defaultConnectTimeout    = 5 * time.Second
)

// Session defaults, applied to every connection in the pool.
const (
	// PgSearchPath lists the schemas created by migration 00000. A schema that
	// does not exist yet is ignored by Postgres, so a pre-migration connection
	// still succeeds.
	PgSearchPath = "public,internal,reference"
	// PgTimezone keeps every session in UTC so timestamps never depend on the
	// host or container clock.
	PgTimezone = "UTC"
)

// ErrNoRows is returned by QueryRow.Scan when the query matched no rows.
// Repositories compare against this instead of importing pgx themselves.
var ErrNoRows = pgx.ErrNoRows

// ErrMissingDSN is returned when a Postgres handle is opened without a DSN.
var ErrMissingDSN = errors.New("datastore: postgres DSN is required")

// Querier is the query surface shared by the pool and a transaction, so a
// repository can accept either without knowing which one it got.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// PostgresOptions configures the application connection pool. Zero fields fall
// back to the defaults above; only DSN is required.
type PostgresOptions struct {
	DSN               string
	ApplicationName   string
	SearchPath        string
	Timezone          string
	MaxConns          int32
	MinConns          int32
	MaxConnLifetime   time.Duration
	MaxConnIdleTime   time.Duration
	HealthCheckPeriod time.Duration
	ConnectTimeout    time.Duration
}

// Postgres owns the single connection pool of the process. Nothing else may
// open another pool or a raw driver connection.
type Postgres struct {
	pool *pgxpool.Pool
	cfg  *pgxpool.Config
	opts PostgresOptions
}

// NewPostgres opens the pool and verifies connectivity, so an unreachable
// database fails at startup instead of on the first query.
func NewPostgres(ctx context.Context, opts PostgresOptions) (*Postgres, error) {
	cfg, normalized, err := newPoolConfig(opts)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("datastore: open postgres pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, normalized.ConnectTimeout)
	defer cancel()
	err = pool.Ping(pingCtx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("datastore: ping postgres: %w", err)
	}

	return &Postgres{pool: pool, cfg: cfg, opts: normalized}, nil
}

// OpenMigrationDB opens the single-connection handle used by goose, without
// building a pool. The caller owns the returned handle and must close it.
func OpenMigrationDB(ctx context.Context, opts PostgresOptions) (*sql.DB, error) {
	cfg, normalized, err := newPoolConfig(opts)
	if err != nil {
		return nil, err
	}
	return openMigrationDB(ctx, cfg.ConnConfig, normalized.ConnectTimeout)
}

// MigrationDB opens the single-connection handle from the running pool
// configuration.
func (p *Postgres) MigrationDB(ctx context.Context) (*sql.DB, error) {
	return openMigrationDB(ctx, p.cfg.ConnConfig, p.opts.ConnectTimeout)
}

// Acquire takes a connection out of the pool for work that needs several
// statements on the same backend, such as LISTEN or a session-level advisory
// lock. The caller must release it with conn.Release.
func (p *Postgres) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("datastore: acquire postgres connection: %w", err)
	}
	return conn, nil
}

// Exec runs a statement on the pool.
func (p *Postgres) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

// Query runs a query on the pool.
func (p *Postgres) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow runs a single-row query on the pool.
func (p *Postgres) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// WithTx runs fn inside a transaction. The transaction is committed when fn
// returns nil, and rolled back otherwise — including on panic. The callback
// receives the shared Querier surface, so it passes the transaction to any
// repository, seeder, or store written against it.
func (p *Postgres) WithTx(ctx context.Context, fn func(ctx context.Context, tx Querier) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("datastore: begin transaction: %w", err)
	}

	// Roll back on a context that survives caller cancellation, so an aborted
	// request still releases the transaction. Rolling back after a successful
	// commit is a no-op (pgx.ErrTxClosed).
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("datastore: commit transaction: %w", err)
	}
	return nil
}

// Ping reports whether the database is reachable.
func (p *Postgres) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

// Stats exposes pool counters for health reporting.
func (p *Postgres) Stats() *pgxpool.Stat {
	return p.pool.Stat()
}

// Shutdown drains and closes the pool, and is what the container calls when
// the run ends.
//
// It is named Shutdown rather than Close because that is the interface
// samber/do looks for: the container calls `Shutdown` on every service that
// implements it and ignores `Close` entirely, so a pool exposing only Close
// was never closed by a run that shut down through the container. There is no
// error to report — pgxpool.Close does not fail — so this is the
// context-only form of the interface.
func (p *Postgres) Shutdown(context.Context) {
	// A handle that was never opened is not a failure to report: the
	// container calls this on whatever it holds, and a test that registers a
	// stub registers a zero value.
	if p == nil || p.pool == nil {
		return
	}
	p.pool.Close()
}

func newPoolConfig(opts PostgresOptions) (*pgxpool.Config, PostgresOptions, error) {
	if opts.DSN == "" {
		return nil, opts, ErrMissingDSN
	}

	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, opts, fmt.Errorf("datastore: parse postgres DSN: %w", err)
	}

	opts = opts.normalize()
	if opts.MinConns < 0 {
		return nil, opts, fmt.Errorf("datastore: postgres min conns must not be negative: %d", opts.MinConns)
	}
	if opts.MinConns > opts.MaxConns {
		return nil, opts, fmt.Errorf(
			"datastore: postgres min conns (%d) exceeds max conns (%d)", opts.MinConns, opts.MaxConns)
	}

	cfg.MaxConns = opts.MaxConns
	cfg.MinConns = opts.MinConns
	cfg.MaxConnLifetime = opts.MaxConnLifetime
	cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	cfg.HealthCheckPeriod = opts.HealthCheckPeriod
	cfg.ConnConfig.ConnectTimeout = opts.ConnectTimeout

	// Session parameters must live in RuntimeParams, not in a startup query:
	// pgxpool resets a reused connection with these, so the pool never hands
	// out a connection on the wrong search_path or timezone.
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = make(map[string]string, 3)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = opts.SearchPath
	cfg.ConnConfig.RuntimeParams["timezone"] = opts.Timezone
	if opts.ApplicationName != "" {
		cfg.ConnConfig.RuntimeParams["application_name"] = opts.ApplicationName
	}
	return cfg, opts, nil
}

func (o PostgresOptions) normalize() PostgresOptions {
	if o.MaxConns <= 0 {
		o.MaxConns = defaultMaxConns
	}
	if o.MinConns == 0 {
		o.MinConns = min(defaultMinConns, o.MaxConns)
	}
	if o.MaxConnLifetime <= 0 {
		o.MaxConnLifetime = defaultMaxConnLifetime
	}
	if o.MaxConnIdleTime <= 0 {
		o.MaxConnIdleTime = defaultMaxConnIdleTime
	}
	if o.HealthCheckPeriod <= 0 {
		o.HealthCheckPeriod = defaultHealthCheckPeriod
	}
	if o.ConnectTimeout <= 0 {
		o.ConnectTimeout = defaultConnectTimeout
	}
	if o.SearchPath == "" {
		o.SearchPath = PgSearchPath
	}
	if o.Timezone == "" {
		o.Timezone = PgTimezone
	}
	return o
}

// openMigrationDB builds a database/sql handle pinned to one connection.
// Migrations must not run on the pool: goose takes a session advisory lock and
// executes multi-statement files, and both require every statement of a run to
// land on the same backend. Idle and lifetime reaping are disabled so that the
// single connection stays open for the whole run.
func openMigrationDB(ctx context.Context, connConfig *pgx.ConnConfig, connectTimeout time.Duration) (*sql.DB, error) {
	db := stdlib.OpenDB(*connConfig)
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)
	db.SetConnMaxLifetime(0)

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("datastore: open postgres migration connection: %w", err)
	}
	return db, nil
}
