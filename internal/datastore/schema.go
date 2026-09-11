// Package datastore bootstraps connections to the application's
// stateful data services. Postgres is the sole database target;
// Redis/Valkey (cache + session store) is a later addition. The
// package owns connection lifecycle only — pools, health checks,
// transactions, graceful shutdown. Domain queries live in the
// modules that own the data: each module builds its own store_*.go
// files on top of the backend handed out by the registry.
//
// Files:
//
//	schema.go   — scope + contracts (this file)
//	postgres.go — Postgres backend (pgx/v5 pool), migrations hook
package datastore

import (
	"context"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Executor is the query surface a module store receives: a pool
// (autocommit) or an open transaction. pgx types are used directly —
// the backend deliberately stays thin instead of re-wrapping a
// mature API behind another abstraction layer.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store is the Backend handed to the composition root. WithTx runs
// fn atomically: the Executor it receives is an open transaction
// that commits when fn returns nil and rolls back on any error.
// Nested transactions are impossible by construction — Executor
// carries no WithTx.
type Store interface {
	Backend
	Executor

	// WithTx executes fn inside a single transaction. Use the
	// Executor argument inside fn — using the Store there would
	// escape the transaction boundary.
	WithTx(ctx context.Context, fn func(Executor) error) error
}

// Backend is the lifecycle contract for every datastore backend:
// health probing plus graceful shutdown. Concrete backends are
// wired in internal/registry and injected into modules through
// registry.Deps — never accessed as globals.
type Backend interface {
	HealthCheck(ctx context.Context) error
	io.Closer
}
