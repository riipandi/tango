package launcher

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
)

// The db migrate command surface: up, down, status, and version
// run in both build variants; create, fix, validate, and reset
// are debug-only (see db_migrate_debug.go). Command implementations
// call the database package, whose own build variants select the
// migration source (disk vs embedded).
//
// Destructive commands (down, reset) confirm on stdin before they
// run; --force skips the prompt and --dry-run prints the target
// without executing anything.

// dbTimeout bounds every database command: migrations, dumps,
// restores, and client-binary invocations. Long restores on big
// databases can exceed it — the context only cancels the wait,
// and pg_dump/pg_restore finish their current statement.
const dbTimeout = 5 * time.Minute

// dbContext returns a timeout-bounded context for database
// commands, so a hung server or binary cannot block the CLI
// forever.
func dbContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), dbTimeout)
}

// migrateConfig loads the layered configuration for database
// commands: the DSN plus the derived data-root paths. Global CLI
// flags (--data-dir) win over env and env-file layers.
func migrateConfig(cli *CLI) (*config.Config, error) {
	return loadConfig(cli, nil)
}

// MigrateUpCmd applies all pending migrations.
type MigrateUpCmd struct {
	// To applies migrations only up to this version (inclusive);
	// 0 means all pending.
	To int64 `help:"Apply migrations only up to this version (default: all)"`
}

// Run applies all pending migrations to the configured database.
func (c *MigrateUpCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	dsn := cfg.Database.URL

	var applied []database.MigrationOutcome
	if c.To > 0 {
		applied, err = database.MigrateUpTo(ctx, dsn, c.To)
	} else {
		applied, err = database.MigrateUp(ctx, dsn)
	}
	if err != nil {
		return fmt.Errorf("db migrate:up: %w", err)
	}
	if len(applied) == 0 {
		fmt.Printf("%snothing to migrate%s\n", colorCyan, colorReset)
		return nil
	}
	for _, outcome := range applied {
		fmt.Printf("%sapplied%s %s (%s)\n", colorGreen, colorReset, outcome.Path, outcome.Duration)
	}
	return nil
}

// MigrateDownCmd rolls back the most recent migration.
type MigrateDownCmd struct {
	// Count rolls back this many migrations (default 1).
	Count  int  `help:"Number of migrations to roll back (default: 1)"`
	Force  bool `help:"Skip the confirmation prompt"`
	DryRun bool `help:"Print what would be rolled back without changing anything"`
}

// Run rolls back the most recent migration after confirmation.
func (c *MigrateDownCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	dsn := cfg.Database.URL

	if c.DryRun {
		return migrateDownDryRun(ctx, dsn, c.Count)
	}
	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}

	outcomes, downErr := migrateDownN(ctx, dsn, c.Count)
	if downErr != nil {
		return fmt.Errorf("db migrate:down: %w", downErr)
	}
	for _, outcome := range outcomes {
		fmt.Printf("%srolled back%s %s (%s)\n", colorGreen, colorReset, outcome.Path, outcome.Duration)
	}
	return nil
}

// migrateDownDryRun reports what down would roll back, without
// touching the database: the last Count applied migrations.
func migrateDownDryRun(ctx context.Context, dsn string, count int) error {
	if count < 1 {
		count = 1
	}
	statuses, err := database.MigrateStatus(ctx, dsn)
	if err != nil {
		return fmt.Errorf("db migrate:down: %w", err)
	}
	targets := appliedDescending(statuses)
	if len(targets) == 0 {
		fmt.Printf("%snothing to roll back%s\n", colorCyan, colorReset)
		return nil
	}
	if count > len(targets) {
		count = len(targets)
	}
	fmt.Printf("%sdry-run%s would roll back %d migration(s):", colorCyan, colorReset, count)
	for _, entry := range targets[:count] {
		fmt.Printf("\n  %s", entry.Path)
	}
	fmt.Println()
	return nil
}

// migrateDownN rolls back Count migrations (at least one): the
// equivalent of repeating goose down Count times, in one operation.
func migrateDownN(ctx context.Context, dsn string, count int) ([]database.MigrationOutcome, error) {
	if count < 1 {
		count = 1
	}
	statuses, err := database.MigrateStatus(ctx, dsn)
	if err != nil {
		return nil, err
	}
	targets := appliedDescending(statuses)
	if len(targets) == 0 {
		return nil, fmt.Errorf("no applied migrations to roll back")
	}
	if count > len(targets) {
		count = len(targets)
	}
	// Roll back everything above the Count-th applied version.
	return database.MigrateDownTo(ctx, dsn, targets[count-1].Version-1)
}

// appliedDescending sorts applied migrations by version, newest
// first.
func appliedDescending(statuses []database.MigrationStatus) []database.MigrationStatus {
	var applied []database.MigrationStatus
	for _, entry := range statuses {
		if entry.State == "applied" {
			applied = append(applied, entry)
		}
	}
	sort.Slice(applied, func(i, j int) bool {
		return applied[i].Version > applied[j].Version
	})
	return applied
}

// MigrateVersionCmd prints the current migration version.
type MigrateVersionCmd struct{}

// Run prints the current applied and target migration versions.
func (c *MigrateVersionCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	current, target, err := database.MigrateVersion(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("db migrate:version: %w", err)
	}
	fmt.Printf("current: %d\ntarget:  %d\n", current, target)
	return nil
}

// MigrateStatusCmd prints the migration status table.
type MigrateStatusCmd struct{}

// Run prints the migration status table.
func (c *MigrateStatusCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	statuses, err := database.MigrateStatus(ctx, cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("db migrate:status: %w", err)
	}

	fmt.Printf("%-8s  %-9s  %-40s  %s\n", "VERSION", "STATE", "MIGRATION", "APPLIED AT")
	for _, entry := range statuses {
		appliedAt := "-"
		if !entry.AppliedAt.IsZero() {
			appliedAt = entry.AppliedAt.UTC().Format("2006-01-02 15:04:05 UTC")
		}
		fmt.Printf("%-8d  %-9s  %-40s  %s\n", entry.Version, entry.State, entry.Path, appliedAt)
	}
	return nil
}
