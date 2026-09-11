package launcher

import (
	"context"
	"fmt"

	"github.com/riipandi/tango/database"
)

// DBCmd groups the database backup and restore operations under
// "migrate db". It shells out to the pg_dump/pg_restore/psql
// binaries and writes to the storage/backup directory.
type DBCmd struct {
	Dump    DBDumpCmd    `cmd:"" help:"Dump schema & data or data only (custom format)"`
	Restore DBRestoreCmd `cmd:"" help:"Restore from a dump file (custom format)"`
	Export  DBExportCmd  `cmd:"" help:"Export schema & data or data only (SQL format)"`
	Import  DBImportCmd  `cmd:"" help:"Import from a SQL file"`
}

// DBDumpCmd creates a binary-format backup.
type DBDumpCmd struct {
	Mode string `arg:"" help:"Dump scope: all (schema & data) or data"`
}

// Run creates a custom-format dump in storage/backup.
func (c *DBDumpCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	path, err := database.Dump(context.Background(), cfg.Database.URL, c.Mode)
	if err != nil {
		return fmt.Errorf("migrate db dump: %w", err)
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
			return fmt.Errorf("migrate db restore: %w", err)
		}
		fmt.Printf("%sdry-run%s %s\n", colorCyan, colorReset, cmd)
		return nil
	}
	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	if err := database.Restore(context.Background(), cfg.Database.URL, c.Mode, c.File); err != nil {
		return fmt.Errorf("migrate db restore: %w", err)
	}
	fmt.Printf("%srestore completed%s\n", colorGreen, colorReset)
	return nil
}

// DBExportCmd creates a plain-SQL backup.
type DBExportCmd struct {
	Mode string `arg:"" help:"Export scope: all (schema & data) or data"`
}

// Run exports the database as SQL in storage/backup.
func (c *DBExportCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	path, err := database.Export(context.Background(), cfg.Database.URL, c.Mode)
	if err != nil {
		return fmt.Errorf("migrate db export: %w", err)
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
			return fmt.Errorf("migrate db import: %w", err)
		}
		fmt.Printf("%sdry-run%s %s\n", colorCyan, colorReset, cmd)
		return nil
	}
	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	if err := database.Import(context.Background(), cfg.Database.URL, c.File); err != nil {
		return fmt.Errorf("migrate db import: %w", err)
	}
	fmt.Printf("%simport completed%s\n", colorGreen, colorReset)
	return nil
}
