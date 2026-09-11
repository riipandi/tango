// Package datastore bootstraps connections to the application's
// stateful data services. Postgres is the sole database target;
// Redis/Valkey (cache + session store) is a later addition. The
// package owns connection lifecycle only — pools, health checks,
// graceful shutdown. Domain queries live in the modules that own
// the data: each module builds its own store_*.go files on top of
// the backend handed out by the registry.
//
// Files:
//
//	schema.go   — scope + backend contract (this file)
//	postgres.go — Postgres backend (pgx/v5 pool), migrations hook
package datastore

import (
	"context"
	"io"
)

// Backend is the lifecycle contract for every datastore backend:
// health probing plus graceful shutdown. Concrete backends are
// wired in internal/registry and injected into modules through
// registry.Deps — never accessed as globals.
type Backend interface {
	HealthCheck(ctx context.Context) error
	io.Closer
}
