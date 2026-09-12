// Package datastore owns connection lifecycle: pools, health,
// transactions, shutdown. Postgres is the only backend; domain
// queries live in the modules owning the data.
package datastore

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Executor is the query surface: a pool (autocommit) or an open
// tx. pgx types used directly; no re-wrapping.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the Backend for the composition root. WithTx runs fn
// atomically; use its Executor arg, not the Store, to stay inside
// the tx. No nesting: Executor has no WithTx.
type Store interface {
	Backend
	Executor

	WithTx(ctx context.Context, fn func(Executor) error) error

	// Pool exposes the raw pgx pool. Domain modules never use it.
	Pool() *pgxpool.Pool
}

// Backend is health plus shutdown. Wired in internal/registry,
// injected via registry.Deps, never globals.
type Backend interface {
	HealthCheck(ctx context.Context) error
	io.Closer
}
