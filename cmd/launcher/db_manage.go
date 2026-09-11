package launcher

import (
	"context"
	"fmt"

	"github.com/riipandi/tango/database"
)

// The db manage command surface: dump, restore, export, and import
// run in both build variants. Every write lands under the
// configured data root (<data-dir>/backup). Restore and import
// replace database content and are gated by confirm/--force/--dry-run.

// DBDumpCmd creates a binary-format backup.
type DBDumpCmd struct {
	Mode string `arg:"" help:"Dump scope: all (schema & data) or data"`
}

// Run creates a custom-format dump in the configured backup dir.
func (c *DBDumpCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	path, err := database.Dump(ctx, cfg.Database.URL, c.Mode, cfg.BackupDir())
	if err != nil {
		return fmt.Errorf("db dump: %w", err)
	}
	fmt.Printf("%sdumped%s %s\n", colorGreen, colorReset, path)
	return nil
}

// DBRestoreCmd restores from a binary-format dump.
type DBRestoreCmd struct {
	Mode   string `arg:"" help:"Restore scope: all, data, or schema"`
	File   string `arg:"" help:"Path to the .dump file"`
	Force  bool   `help:"Skip the confirmation prompt"`
	DryRun bool   `help:"Print the pg_restore command without running it"`
}

// Run restores the database from a dump after confirmation.
func (c *DBRestoreCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	if c.DryRun {
		cmd, err := database.RestoreCommand(cfg.Database.URL, c.Mode, c.File)
		if err != nil {
			return fmt.Errorf("db restore: %w", err)
		}
		fmt.Printf("%sdry-run%s %s\n", colorCyan, colorReset, cmd)
		return nil
	}
	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	ctx, cancel := dbContext()
	defer cancel()
	if err := database.Restore(ctx, cfg.Database.URL, c.Mode, c.File); err != nil {
		return fmt.Errorf("db restore: %w", err)
	}
	fmt.Printf("%srestore completed%s\n", colorGreen, colorReset)
	return nil
}

// DBExportCmd creates a plain-SQL backup.
type DBExportCmd struct {
	Mode string `arg:"" help:"Export scope: all (schema & data) or data"`
}

// Run exports the database as SQL into the configured backup dir.
func (c *DBExportCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	path, err := database.Export(ctx, cfg.Database.URL, c.Mode, cfg.BackupDir())
	if err != nil {
		return fmt.Errorf("db export: %w", err)
	}
	fmt.Printf("%sexported%s %s\n", colorGreen, colorReset, path)
	return nil
}

// DBImportCmd imports a plain SQL file.
type DBImportCmd struct {
	File   string `arg:"" help:"Path to the .sql file"`
	Force  bool   `help:"Skip the confirmation prompt"`
	DryRun bool   `help:"Print the psql command without running it"`
}

// Run imports a SQL file after confirmation.
func (c *DBImportCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	if c.DryRun {
		cmd, err := database.ImportCommand(cfg.Database.URL, c.File)
		if err != nil {
			return fmt.Errorf("db import: %w", err)
		}
		fmt.Printf("%sdry-run%s %s\n", colorCyan, colorReset, cmd)
		return nil
	}
	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	ctx, cancel := dbContext()
	defer cancel()
	if err := database.Import(ctx, cfg.Database.URL, c.File); err != nil {
		return fmt.Errorf("db import: %w", err)
	}
	fmt.Printf("%simport completed%s\n", colorGreen, colorReset)
	return nil
}

// dbRestoreDryRun renders the restore invocation without running
// it.
func dbRestoreDryRun(ctx context.Context, dsn, mode, file string) error {
	cmd, err := database.RestoreCommand(dsn, mode, file)
	if err != nil {
		return fmt.Errorf("db restore: %w", err)
	}
	fmt.Printf("%sdry-run%s %s\n", colorCyan, colorReset, cmd)
	return nil
}
