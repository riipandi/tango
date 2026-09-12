// Goose runner shared by both variants. Builds a Provider over the
// source picked by the variant files, exposes package-owned types
// (goose never leaks). --dry-run reads via the same Provider API.
package database

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var DatabaseMigrations embed.FS

// Goose metadata table; excluded from get_table_sizes().
const appMigrationTableName = "app_migration"

// MigrationOutcome is one applied or rolled-back migration.
type MigrationOutcome struct {
	Version  int64
	Path     string
	Duration time.Duration
}

// MigrationStatus is one migration file's state.
type MigrationStatus struct {
	Version   int64
	Path      string
	State     string // "applied" or "pending"
	AppliedAt time.Time
}

// openProvider opens the db and builds a Provider over src.
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

// runStatus reports every migration file's state.
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

// runDownTarget reports what runDown would roll back next,
// read-only. Nil means nothing applied.
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

// runUpTo applies pending migrations up to version (inclusive).
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

// runDownTo rolls back everything above version.
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

// runVersion reports applied and file-system target versions.
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

// outcomeFrom converts a goose result to the package type.
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
