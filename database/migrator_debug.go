//go:build debug

package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/pressly/goose/v3"
)

// devMigrationsDir is the on-disk migrations directory relative to
// the repository root — the working directory of debug runs
// (`task dev`, `go run -tags debug ./cmd`).
const devMigrationsDir = "database/migrations"

// The debug migration surface: every operation, reading migrations
// from the on-disk directory so newly created files apply without
// rebuilding the binary.

// MigrateUp applies all pending migrations.
func MigrateUp(ctx context.Context, dsn string) ([]MigrationOutcome, error) {
	return runUp(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, dsn string) (*MigrationOutcome, error) {
	return runDown(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateDownTarget reports what MigrateDown would roll back next,
// without touching the database.
func MigrateDownTarget(ctx context.Context, dsn string) (*MigrationStatus, error) {
	return runDownTarget(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateStatus reports the state of every migration file.
func MigrateStatus(ctx context.Context, dsn string) ([]MigrationStatus, error) {
	return runStatus(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateReset rolls every migration back, returning the database
// to its initial state.
func MigrateReset(ctx context.Context, dsn string) ([]MigrationOutcome, error) {
	return runReset(ctx, dsn, os.DirFS(devMigrationsDir))
}

// runReset rolls every migration back (debug-only operation).
func runReset(ctx context.Context, dsn string, src fs.FS) ([]MigrationOutcome, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := p.DownTo(ctx, 0)
	if err != nil {
		return nil, fmt.Errorf("reset migrations: %w", err)
	}
	outcomes := make([]MigrationOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, outcomeFrom(result))
	}
	return outcomes, nil
}

// withMigrationsDB opens the database connection (lazy — no server
// contact), runs fn, and closes it. Used by file scaffolding, which
// never needs a live server.
func withMigrationsDB(dsn string, fn func(*sql.DB) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	return fn(db)
}

// CreateMigration scaffolds the next sequential SQL migration file
// in the on-disk migrations directory and returns its path.
func CreateMigration(dsn, name string) (string, error) {
	return createMigration(dsn, name, devMigrationsDir)
}

// createMigration scaffolds into an explicit directory. The created
// path is derived by diffing the directory across goose.Create, so
// the naming logic stays goose's own.
func createMigration(dsn, name, dir string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("migration name is required")
	}

	before := dirEntries(dir)
	err := withMigrationsDB(dsn, func(db *sql.DB) error {
		goose.SetLogger(goose.NopLogger())
		goose.SetSequential(true)
		return goose.Create(db, dir, name, "sql")
	})
	if err != nil {
		return "", err
	}

	for _, entry := range dirEntries(dir) {
		if !slices.Contains(before, entry) {
			return entry, nil
		}
	}
	return "", fmt.Errorf("scaffolded migration not found in %s", dir)
}

// dirEntries lists the .sql files in dir by name.
func dirEntries(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}
