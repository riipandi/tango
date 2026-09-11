package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres-specific constants
const (
	PgSearchPath = "public,auth,reference,scheduler"
	PgTimezone   = "UTC"
)

// Options parametrizes the Postgres backend.
type Options struct {
	// DSN is the Postgres connection string (postgresql:// or
	// postgres://). Pool tuning can also travel in the DSN itself
	// through the pgx query parameters — pool_max_conns,
	// pool_min_conns, pool_max_conn_lifetime, pool_max_conn_idle_time,
	// pool_health_check_period.
	DSN string

	// MaxConnections caps the pool size; 0 keeps the pgx default
	// (or the DSN's pool_max_conns).
	MaxConnections int32

	// MinConnections keeps warm idle connections; 0 keeps the
	// pgx default (or the DSN's pool_min_conns).
	MinConnections int32

	// MaxConnLifetime bounds how long a connection may be reused;
	// 0 keeps the pgx default (or the DSN's pool_max_conn_lifetime).
	MaxConnLifetime time.Duration

	// MaxConnIdleTime closes idle connections after this duration;
	// 0 keeps the pgx default (or the DSN's pool_max_conn_idle_time).
	MaxConnIdleTime time.Duration
}

// Postgres is the pgx/v5 pool-backed Store. It owns the pool
// lifecycle: ping on construction (fail fast), health probing for
// the runtime, and graceful drain on Close.
type Postgres struct {
	pool *pgxpool.Pool
}

// compile-time proof that the concrete backend satisfies both
// contracts handed out by the registry.
var _ Store = (*Postgres)(nil)

// poolConfig parses opts.DSN and applies the backend's connection
// settings: session runtime params, a fixed connect timeout, and the
// optional pool knobs (Options values win over DSN parameters).
func poolConfig(opts Options) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}

	// Configure connection settings
	cfg.ConnConfig.ConnectTimeout = time.Second * 10
	cfg.ConnConfig.RuntimeParams = map[string]string{
		"search_path": PgSearchPath,
		"timezone":    PgTimezone,
	}

	if opts.MaxConnections > 0 {
		cfg.MaxConns = opts.MaxConnections
	}
	if opts.MinConnections > 0 {
		cfg.MinConns = opts.MinConnections
	}
	if opts.MaxConnLifetime > 0 {
		cfg.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.MaxConnIdleTime > 0 {
		cfg.MaxConnIdleTime = opts.MaxConnIdleTime
	}
	return cfg, nil
}

// New builds the pool from opts and verifies connectivity once
// before returning, so startup fails fast on a wrong or unreachable
// database.
func New(ctx context.Context, opts Options) (*Postgres, error) {
	if opts.DSN == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}

	cfg, err := poolConfig(opts)
	if err != nil {
		return nil, err
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: build pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping %s: %w", cfg.ConnConfig.Host, err)
	}

	return &Postgres{pool: pool}, nil
}

// Exec runs a statement with no result rows (autocommit).
func (p *Postgres) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

// Query runs a statement returning multiple rows (autocommit).
func (p *Postgres) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow runs a statement returning at most one row (autocommit).
func (p *Postgres) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// WithTx executes fn inside a single transaction: commit on nil,
// rollback on any error or panic. The rollback runs on an uncanceled
// copy of ctx so a shutdown-path error still releases the connection
// back to the pool instead of leaking it.
func (p *Postgres) WithTx(ctx context.Context, fn func(Executor) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx)) // no-op after commit (ErrTxClosed)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit transaction: %w", err)
	}
	return nil
}

// ConnectionInfo reports what TestConnection learned: where the
// pool actually connected, what answered, and how the pool is
// doing right now.
type ConnectionInfo struct {
	// Host, Port, Database, User come from the resolved pool
	// configuration and the session itself.
	Host     string
	Port     uint16
	Database string
	User     string

	// ServerVersion is the human-readable server version string;
	// ServerVersionNum is the numeric form for capability checks.
	ServerVersion    string
	ServerVersionNum int64

	// Latency is the round-trip duration of the diagnostic query.
	Latency time.Duration

	// Stats is the live pool snapshot (see pgxpool.Stat).
	Stats *pgxpool.Stat
}

// TestConnection runs a round-trip against the server and reports
// connection diagnostics: resolved identity (host, database, user),
// server version, latency, and pool stats. Use it to verify a DSN
// or to feed health endpoints.
func (p *Postgres) TestConnection(ctx context.Context) (*ConnectionInfo, error) {
	start := time.Now()

	var info ConnectionInfo
	err := p.pool.QueryRow(ctx,
		"SELECT version(), current_database(), current_user, current_setting('server_version_num')::bigint",
	).Scan(&info.ServerVersion, &info.Database, &info.User, &info.ServerVersionNum)
	if err != nil {
		return nil, fmt.Errorf("postgres: test connection: %w", err)
	}
	info.Latency = time.Since(start)

	connCfg := p.pool.Config().ConnConfig
	info.Host = connCfg.Host
	info.Port = connCfg.Port
	info.Stats = p.pool.Stat()

	return &info, nil
}

// HealthCheck reports whether the pool can reach the database.
func (p *Postgres) HealthCheck(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: health check: %w", err)
	}
	return nil
}

// Close drains the pool, releasing every idle connection and
// terminating outstanding ones. Safe to call once.
func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
