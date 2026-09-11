// Migrations runner shared by both build variants. It builds a
// goose Provider over the migration source selected by the variant
// files (migrator_debug.go, migrator_release.go) and exposes
// results as package-owned types — goose types never leak past
// this boundary. The provider API is read- and write-oriented, so
// the CLI can implement --dry-run as pure introspection.
package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/pressly/goose/v3"
)

// appMigrationTableName is the goose metadata table. It matches the
// exclusion in the get_table_sizes() helper defined by the initial
// migration.
const appMigrationTableName = "app_migration"

// MigrationOutcome describes one migration the runner just applied
// or rolled back.
type MigrationOutcome struct {
	Version  int64
	Path     string
	Duration time.Duration
}

// MigrationStatus describes the state of one migration file.
type MigrationStatus struct {
	Version   int64
	Path      string
	State     string // "applied" or "pending"
	AppliedAt time.Time
}

// openProvider opens the database and builds a goose Provider over
// src with the project's metadata table.
func openProvider(dsn string, src fs.FS) (*sql.DB, *goose.Provider, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("open database: %w", err)
	}
	p, err := goose.NewProvider("postgres", db, src, goose.WithTableName(appMigrationTableName))
	if err != nil {
		db.Close()
		return nil, nil, fmt.Errorf("build migrator: %w", err)
	}
	return db, p, nil
}

// runUp applies all pending migrations.
func runUp(ctx context.Context, dsn string, src fs.FS) ([]MigrationOutcome, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := p.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}
	outcomes := make([]MigrationOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, outcomeFrom(result))
	}
	return outcomes, nil
}

// runDown rolls back the most recent migration.
func runDown(ctx context.Context, dsn string, src fs.FS) (*MigrationOutcome, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	result, err := p.Down(ctx)
	if err != nil {
		return nil, fmt.Errorf("roll back migration: %w", err)
	}
	return outcomePtrFrom(result), nil
}

// runStatus reports the state of every migration file.
func runStatus(ctx context.Context, dsn string, src fs.FS) ([]MigrationStatus, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	statuses, err := p.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration status: %w", err)
	}
	out := make([]MigrationStatus, 0, len(statuses))
	for _, status := range statuses {
		entry := MigrationStatus{State: string(status.State), AppliedAt: status.AppliedAt}
		if status.Source != nil {
			entry.Version = status.Source.Version
			entry.Path = status.Source.Path
		}
		out = append(out, entry)
	}
	return out, nil
}

// runDownTarget reports the migration runDown would roll back
// next, without touching the database: the highest applied
// version. A nil result means nothing is applied.
func runDownTarget(ctx context.Context, dsn string, src fs.FS) (*MigrationStatus, error) {
	statuses, err := runStatus(ctx, dsn, src)
	if err != nil {
		return nil, err
	}

	var target *MigrationStatus
	for i := range statuses {
		if statuses[i].State == "applied" && (target == nil || statuses[i].Version > target.Version) {
			entry := statuses[i]
			target = &entry
		}
	}
	return target, nil
}

// runUpTo applies pending migrations up to the given version
// (inclusive).
func runUpTo(ctx context.Context, dsn string, src fs.FS, version int64) ([]MigrationOutcome, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := p.UpTo(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("apply migrations to %d: %w", version, err)
	}
	outcomes := make([]MigrationOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, outcomeFrom(result))
	}
	return outcomes, nil
}

// runDownTo rolls back every migration above the given version.
func runDownTo(ctx context.Context, dsn string, src fs.FS, version int64) ([]MigrationOutcome, error) {
	db, p, err := openProvider(dsn, src)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	results, err := p.DownTo(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("roll back to %d: %w", version, err)
	}
	outcomes := make([]MigrationOutcome, 0, len(results))
	for _, result := range results {
		outcomes = append(outcomes, outcomeFrom(result))
	}
	return outcomes, nil
}

// runVersion reports the current applied and the target (file
// system) migration versions.
func runVersion(ctx context.Context, dsn string, src fs.FS) (current, target int64, err error) {
	db, p, openErr := openProvider(dsn, src)
	if openErr != nil {
		return 0, 0, openErr
	}
	defer db.Close()

	current, target, err = p.GetVersions(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("migration version: %w", err)
	}
	return current, target, nil
}

// outcomeFrom converts a goose result into the package type.
func outcomeFrom(result *goose.MigrationResult) MigrationOutcome {
	outcome := MigrationOutcome{Duration: result.Duration}
	if result.Source != nil {
		outcome.Version = result.Source.Version
		outcome.Path = result.Source.Path
	}
	return outcome
}

func outcomePtrFrom(result *goose.MigrationResult) *MigrationOutcome {
	if result == nil {
		return nil
	}
	outcome := outcomeFrom(result)
	return &outcome
}
