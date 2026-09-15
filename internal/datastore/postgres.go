package datastore

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	PgSearchPath = "public,auth,reference,scheduler"
	PgTimezone   = "UTC"
)

// Options configures the Postgres backend. Explicit pool values override DSN values.
type Options struct {
	// DSN is the connection string (postgresql:// or postgres://).
	DSN string

	// MaxConnections caps pool size; 0 keeps pgx/DSN default.
	MaxConnections int32

	// MinConnections keeps warm idle; 0 keeps pgx/DSN default.
	MinConnections int32

	// MaxConnLifetime bounds reuse; 0 keeps pgx/DSN default.
	MaxConnLifetime time.Duration

	// MaxConnIdleTime closes idle conns; 0 keeps pgx/DSN default.
	MaxConnIdleTime time.Duration
}

// Postgres is a pgx/v5 pool-backed Store.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// poolConfig parses the DSN and applies session and pool settings.
func poolConfig(opts Options) (*pgxpool.Config, error) {
	cfg, err := pgxpool.ParseConfig(opts.DSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse DSN: %w", err)
	}

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

// New builds the pool and verifies the connection.
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

// Exec runs a statement outside a transaction.
func (p *Postgres) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

// Query runs a query outside a transaction.
func (p *Postgres) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow runs a single-row query outside a transaction.
func (p *Postgres) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// Pool exposes the pool to infrastructure that needs direct access.
func (p *Postgres) Pool() *pgxpool.Pool {
	return p.pool
}

// WithTx runs fn in a transaction and rolls back on error or panic.
func (p *Postgres) WithTx(ctx context.Context, fn func(Executor) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(context.WithoutCancel(ctx)) // no-op after commit
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit transaction: %w", err)
	}
	return nil
}

// ConnectionInfo contains connection details and pool stats.
type ConnectionInfo struct {
	Host     string
	Port     uint16
	Database string
	User     string

	ServerVersion    string
	ServerVersionNum int64

	Latency time.Duration

	Stats *pgxpool.Stat
}

// TestConnection queries the server and returns connection details.
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

// HealthCheck reports whether the pool can reach the server.
func (p *Postgres) HealthCheck(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("postgres: health check: %w", err)
	}
	return nil
}

// Close closes the pool.
func (p *Postgres) Close() error {
	p.pool.Close()
	return nil
}
