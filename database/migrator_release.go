//go:build !debug

package database

import (
	"context"
	"io/fs"
)

// embeddedMigrationsDir is the migrations directory inside the
// embedded FS (see embed.go).
const embeddedMigrationsDir = "migrations"

// The release migration surface: apply, roll back, and inspect the
// compiled-in migrations (see embed.go). Scaffolding new migration
// files and resetting the schema are development operations and
// stay out of release builds.

// migrationsSource returns the embedded migrations as a single
// filesystem root for the goose Provider.
func migrationsSource() fs.FS {
	sub, err := fs.Sub(DatabaseMigrations, embeddedMigrationsDir)
	if err != nil {
		// The embed pattern is compile-time fixed; a mismatch is a
		// programming error and panics at first use.
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

// MigrateDownTarget reports what MigrateDown would roll back next,
// without touching the database.
func MigrateDownTarget(ctx context.Context, dsn string) (*MigrationStatus, error) {
	return runDownTarget(ctx, dsn, migrationsSource())
}

// MigrateStatus reports the state of every embedded migration.
func MigrateStatus(ctx context.Context, dsn string) ([]MigrationStatus, error) {
	return runStatus(ctx, dsn, migrationsSource())
}
