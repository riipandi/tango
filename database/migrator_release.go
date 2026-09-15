//go:build !debug

// Package database applies/inspects compiled-in migrations.
// Scaffolding and reset are debug-only.
package database

import (
	"context"
	"io/fs"
)

// Migrations dir inside the embedded FS.
const embeddedMigrationsDir = "migrations"

// migrationsSource returns embedded migrations as a Provider root.
func migrationsSource() fs.FS {
	sub, err := fs.Sub(DatabaseMigrations, embeddedMigrationsDir)
	if err != nil {
		// Embed pattern is compile-time fixed; mismatch is a bug.
		panic("database: embedded migrations directory missing: " + err.Error())
	}
	return sub
}

// MigrateUp applies all pending embedded migrations.
func MigrateUp(ctx context.Context, dsn string) ([]MigrationOutcome, error) {
	return runUp(ctx, dsn, migrationsSource())
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, dsn string) (*MigrationOutcome, error) {
	return runDown(ctx, dsn, migrationsSource())
}

// MigrateDownTarget reports what MigrateDown would roll back, read-only.
func MigrateDownTarget(ctx context.Context, dsn string) (*MigrationStatus, error) {
	return runDownTarget(ctx, dsn, migrationsSource())
}

// MigrateStatus reports every embedded migration's state.
func MigrateStatus(ctx context.Context, dsn string) ([]MigrationStatus, error) {
	return runStatus(ctx, dsn, migrationsSource())
}

// MigrateUpTo applies pending migrations up to version.
func MigrateUpTo(ctx context.Context, dsn string, version int64) ([]MigrationOutcome, error) {
	return runUpTo(ctx, dsn, migrationsSource(), version)
}

// MigrateDownTo rolls back everything above version.
func MigrateDownTo(ctx context.Context, dsn string, version int64) ([]MigrationOutcome, error) {
	return runDownTo(ctx, dsn, migrationsSource(), version)
}

// MigrateVersion reports applied and target versions.
func MigrateVersion(ctx context.Context, dsn string) (current, target int64, err error) {
	return runVersion(ctx, dsn, migrationsSource())
}
