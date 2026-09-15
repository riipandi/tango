// Package datastore owns the Postgres pool, transactions, and health checks.
package datastore

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor is the query surface for a pool or transaction.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store combines Backend, Executor, and transaction support.
type Store interface {
	Backend
	Executor

	WithTx(ctx context.Context, fn func(Executor) error) error

	// Pool exposes the raw pgx pool to infrastructure code.
	Pool() *pgxpool.Pool
}

// Backend provides health checks and shutdown.
type Backend interface {
	HealthCheck(ctx context.Context) error
	io.Closer
}
