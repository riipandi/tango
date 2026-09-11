package launcher

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
)

// The migrate command surface. Which subcommands exist is
// build-dependent — debug builds add create and reset (see
// migrate_debug.go and migrate_release.go). Command implementations
// call the database package, whose own build variants select the
// migration source (disk vs embedded).
//
// Destructive commands (down, reset) confirm on stdin before they
// run; --force skips the prompt and --dry-run prints the target
// without executing anything.

// MigrateUpCmd applies all pending migrations.
type MigrateUpCmd struct{}

// Run applies all pending migrations to the configured database.
func (c *MigrateUpCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	applied, err := database.MigrateUp(context.Background(), cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("migrate up: %w", err)
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
	Force  bool `help:"Skip the confirmation prompt"`
	DryRun bool `help:"Print what would be rolled back without changing anything"`
}

// Run rolls back the most recent migration after confirmation.
func (c *MigrateDownCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx := context.Background()
	dsn := cfg.Database.URL

	if c.DryRun {
		target, targetErr := database.MigrateDownTarget(ctx, dsn)
		if targetErr != nil {
			return fmt.Errorf("migrate down: %w", targetErr)
		}
		if target == nil {
			fmt.Printf("%snothing to roll back%s\n", colorCyan, colorReset)
			return nil
		}
		fmt.Printf("%sdry-run%s would roll back %s\n", colorCyan, colorReset, target.Path)
		return nil
	}

	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	outcome, downErr := database.MigrateDown(ctx, dsn)
	if downErr != nil {
		return fmt.Errorf("migrate down: %w", downErr)
	}
	fmt.Printf("%srolled back%s %s (%s)\n", colorGreen, colorReset, outcome.Path, outcome.Duration)
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
	statuses, err := database.MigrateStatus(context.Background(), cfg.Database.URL)
	if err != nil {
		return fmt.Errorf("migrate status: %w", err)
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

// stdinReader is the input source for confirmation prompts; tests
// override it the same way captureStdout swaps os.Stdout.
var stdinReader io.Reader = os.Stdin

// stdinIsInteractive reports whether stdin is a terminal. Pipes and
// /dev/null cannot confirm a prompt.
var stdinIsInteractive = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// confirmDestructive gates destructive migrations: unless --force,
// it prompts on stdin and refuses in non-interactive sessions (CI,
// scripts), where nobody can answer the prompt.
func confirmDestructive(force bool) error {
	if force {
		return nil
	}
	if !stdinIsInteractive() {
		return fmt.Errorf("refusing destructive migration in a non-interactive session (pass --force to proceed)")
	}

	fmt.Print("This operation is destructive. Proceed? [y/N] ")
	answer, err := bufio.NewReader(stdinReader).ReadString('\n')
	if err != nil && answer == "" {
		return fmt.Errorf("aborted")
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "y" || answer == "yes" {
		return nil
	}
	return fmt.Errorf("aborted")
}

// migrateConfig loads the layered configuration for migration
// commands: only the DSN matters.
func migrateConfig(cli *CLI) (*config.Config, error) {
	cfg, err := config.Load(config.LoadOptions{EnvFile: cli.EnvFile})
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}
