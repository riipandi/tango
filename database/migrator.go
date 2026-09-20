// Package database runs the goose migrations embedded in the binary.
package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// VersionTable records the applied migrations. goose defaults to
// goose_db_version; the project owns the name so the schema reads as ours.
// The table resolves through the connection's current_schema().
const VersionTable = "app_migration"

// MigratorOptions configures a Migrator.
type MigratorOptions struct {
	// AllowOutOfOrder applies migrations that are missing below the current
	// database version instead of failing. Without it goose refuses to run,
	// because an older migration may depend on a schema the newer ones changed.
	AllowOutOfOrder bool
}

// Migrator applies the embedded migrations over a single-connection handle.
type Migrator struct {
	provider *goose.Provider
}

// Migration is one migration goose executed.
type Migration struct {
	Version  int64
	Name     string
	Duration time.Duration
	Empty    bool
}

// MigrationStatus is one migration known to the binary and whether the
// database has it.
type MigrationStatus struct {
	Version   int64
	Name      string
	Applied   bool
	AppliedAt time.Time
}

// NewMigrator loads the embedded migrations and prepares the provider. db must
// be the single-connection handle from datastore.OpenMigrationDB: goose takes a
// session advisory lock on it, so a pool could hand the lock and the migration
// statements to different backends.
func NewMigrator(ctx context.Context, db *sql.DB, opts MigratorOptions) (*Migrator, error) {
	if db == nil {
		return nil, errors.New("database: migrator requires a database handle")
	}

	fsys, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return nil, fmt.Errorf("database: open embedded migrations: %w", err)
	}

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return nil, fmt.Errorf("database: create migration locker: %w", err)
	}

	provider, err := goose.NewProvider(goose.DialectPostgres, db, fsys,
		goose.WithSessionLocker(locker),
		goose.WithTableName(VersionTable),
		goose.WithAllowOutofOrder(opts.AllowOutOfOrder),
		// The command prints the results itself, in the same shape as the rest
		// of the CLI, so goose must not log them a second time.
		goose.WithLogger(goose.NopLogger()),
	)
	if err != nil {
		return nil, fmt.Errorf("database: load migrations: %w", err)
	}
	return &Migrator{provider: provider}, nil
}

// Up applies every pending migration. An already up-to-date database returns an
// empty slice.
func (m *Migrator) Up(ctx context.Context) ([]Migration, error) {
	results, err := m.provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("database: migrate up: %w", err)
	}
	return convert(results), nil
}

// UpTo applies pending migrations up to and including version.
func (m *Migrator) UpTo(ctx context.Context, version int64) ([]Migration, error) {
	results, err := m.provider.UpTo(ctx, version)
	if err != nil {
		return nil, fmt.Errorf("database: migrate up to %d: %w", version, err)
	}
	return convert(results), nil
}

// Status lists every embedded migration with its applied state, in version
// order.
func (m *Migrator) Status(ctx context.Context) ([]MigrationStatus, error) {
	statuses, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("database: read migration status: %w", err)
	}

	out := make([]MigrationStatus, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, MigrationStatus{
			Version:   status.Source.Version,
			Name:      filepath.Base(status.Source.Path),
			Applied:   status.State == goose.StateApplied,
			AppliedAt: status.AppliedAt,
		})
	}
	return out, nil
}

// Version returns the highest version recorded in the database, or 0 when the
// database has never been migrated.
func (m *Migrator) Version(ctx context.Context) (int64, error) {
	version, err := m.provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("database: read database version: %w", err)
	}
	return version, nil
}

// Pending lists the migrations the database has not applied yet, in version
// order.
func (m *Migrator) Pending(ctx context.Context) ([]MigrationStatus, error) {
	statuses, err := m.Status(ctx)
	if err != nil {
		return nil, err
	}

	pending := make([]MigrationStatus, 0, len(statuses))
	for _, status := range statuses {
		if !status.Applied {
			pending = append(pending, status)
		}
	}
	return pending, nil
}

// HighestVersion returns the version of the last embedded migration, which is
// the version the database reaches after a full up.
func (m *Migrator) HighestVersion() int64 {
	sources := m.provider.ListSources()
	if len(sources) == 0 {
		return 0
	}
	return sources[len(sources)-1].Version
}

func convert(results []*goose.MigrationResult) []Migration {
	out := make([]Migration, 0, len(results))
	for _, result := range results {
		out = append(out, Migration{
			Version:  result.Source.Version,
			Name:     filepath.Base(result.Source.Path),
			Duration: result.Duration,
			Empty:    result.Empty,
		})
	}
	return out
}
