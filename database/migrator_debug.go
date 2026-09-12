//go:build debug

package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/pressly/goose/v3"
)

// On-disk migrations dir, relative to repo root (debug cwd).
const devMigrationsDir = "database/migrations"

// Debug reads on-disk so new files apply without rebuild.

// MigrateUp applies all pending migrations.
func MigrateUp(ctx context.Context, dsn string) ([]MigrationOutcome, error) {
	return runUp(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateDown rolls back the most recent migration.
func MigrateDown(ctx context.Context, dsn string) (*MigrationOutcome, error) {
	return runDown(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateDownTarget reports what MigrateDown would roll back, read-only.
func MigrateDownTarget(ctx context.Context, dsn string) (*MigrationStatus, error) {
	return runDownTarget(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateStatus reports every migration file's state.
func MigrateStatus(ctx context.Context, dsn string) ([]MigrationStatus, error) {
	return runStatus(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateUpTo applies pending migrations up to version.
func MigrateUpTo(ctx context.Context, dsn string, version int64) ([]MigrationOutcome, error) {
	return runUpTo(ctx, dsn, os.DirFS(devMigrationsDir), version)
}

// MigrateDownTo rolls back everything above version.
func MigrateDownTo(ctx context.Context, dsn string, version int64) ([]MigrationOutcome, error) {
	return runDownTo(ctx, dsn, os.DirFS(devMigrationsDir), version)
}

// MigrateVersion reports applied and target versions.
func MigrateVersion(ctx context.Context, dsn string) (current, target int64, err error) {
	return runVersion(ctx, dsn, os.DirFS(devMigrationsDir))
}

// MigrateReset rolls every migration back. Debug-only.
func MigrateReset(ctx context.Context, dsn string) ([]MigrationOutcome, error) {
	return runReset(ctx, dsn, os.DirFS(devMigrationsDir))
}

// runReset rolls back to version 0.
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

// withMigrationsDB opens lazily (no server contact) for scaffolding.
func withMigrationsDB(dsn string, fn func(*sql.DB) error) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	return fn(db)
}

// CreateMigration scaffolds the next sequential file, returns its path.
func CreateMigration(dsn, name string) (string, error) {
	return createMigration(dsn, name, devMigrationsDir)
}

// createMigration scaffolds into dir; the path comes from diffing
// across goose.Create, so naming stays goose's own.
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

// Fix reorders files sequentially (goose fix).
func Fix() error {
	return goose.Fix(devMigrationsDir)
}

// Validate checks naming, duplicates, Up/Down annotations.
func Validate() error {
	return validateDir(devMigrationsDir)
}

// validateDir validates an explicit directory.
func validateDir(dir string) error {
	migrations := dirEntries(dir)
	if len(migrations) == 0 {
		return fmt.Errorf("no migration files found in %s", dir)
	}

	seen := make(map[int64]bool, len(migrations))
	var problems []string
	for _, name := range migrations {
		version, err := migrationVersion(name)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if seen[version] {
			problems = append(problems, fmt.Sprintf("%s: duplicate version %d", name, version))
		}
		seen[version] = true

		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		body := string(data)
		for _, annotation := range []string{"+goose Up", "+goose Down"} {
			if !strings.Contains(body, annotation) {
				problems = append(problems, fmt.Sprintf("%s: missing %q annotation", name, annotation))
			}
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("%d problem(s): %s", len(problems), strings.Join(problems, "; "))
	}
	return nil
}

// migrationVersion parses the "%05d_" prefix. Short/malformed names
// are validation errors, never a slice panic.
func migrationVersion(name string) (int64, error) {
	if len(name) < 6 || !strings.HasPrefix(name[5:], "_") {
		return 0, fmt.Errorf("expected sequential naming like 00001_name.sql")
	}
	version, err := strconv.ParseInt(name[:5], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("expected sequential naming like 00001_name.sql")
	}
	return version, nil
}

// dirEntries lists .sql files in dir by name.
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
