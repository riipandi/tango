package datastore

// TODO: Postgres backend — the only database store.
//
// Wrap *pgxpool.Pool (github.com/jackc/pgx/v5/pgxpool) behind the
// Backend contract:
//
//	New(ctx, cfg) — build the pool from the configured DSN (pool
//	size, statement timeout), ping once, return a ready backend.
//	HealthCheck   — pool.Ping.
//	Close         — pool.Close.
//
// DSN and pool limits come from the config package (new section,
// e.g. datastore.postgres.*). Migrations run through the planned
// kernel.Migrator integration; modules own their tables and never
// see the pool outside registry.Deps-injected store constructors.
