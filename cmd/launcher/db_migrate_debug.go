//go:build debug

package launcher

import (
	"context"
	"fmt"

	"github.com/riipandi/tango/database"
)

// Debug DBCmd: manage plus dev-only create/fix/validate/reset.
type DBCmd struct {
	Dump           DBDumpCmd          `cmd:"" help:"Dump schema & data or data only (custom format)"`
	Export         DBExportCmd        `cmd:"" help:"Export schema & data or data only (SQL format)"`
	Import         DBImportCmd        `cmd:"" help:"Import from a SQL file"`
	MigrateUp      MigrateUpCmd       `cmd:"" name:"migrate:up" help:"Run database migrations"`
	MigrateDown    MigrateDownCmd     `cmd:"" name:"migrate:down" help:"Rollback the most recent migration"`
	MigrateStatus  MigrateStatusCmd   `cmd:"" name:"migrate:status" help:"Check database migration status"`
	MigrateVersion MigrateVersionCmd  `cmd:"" name:"migrate:version" help:"Print the current migration version"`
	MigrateCreate  MigrateCreateCmd   `cmd:"" name:"migrate:create" help:"Create a new sequential migration file"`
	MigrateFix     MigrateFixCmd      `cmd:"" name:"migrate:fix" help:"Reorder migration files"`
	MigrateVal     MigrateValidateCmd `cmd:"" name:"migrate:validate" help:"Check the migration files"`
	MigrateReset   MigrateResetCmd    `cmd:"" name:"migrate:reset" help:"Rollback all migrations"`
	Restore        DBRestoreCmd       `cmd:"" help:"Restore from a dump file (custom format)"`
}

// MigrateFixCmd reorders migration files.
type MigrateFixCmd struct{}

// Run reorders migration files sequentially.
func (c *MigrateFixCmd) Run(cli *CLI) error {
	if err := database.Fix(); err != nil {
		return fmt.Errorf("db migrate:fix: %w", err)
	}
	fmt.Printf("%sfixed%s migration file ordering\n", colorGreen, colorReset)
	return nil
}

// MigrateValidateCmd checks migration files.
type MigrateValidateCmd struct{}

// Run validates naming and annotations.
func (c *MigrateValidateCmd) Run(cli *CLI) error {
	if err := database.Validate(); err != nil {
		return fmt.Errorf("db migrate:validate: %w", err)
	}
	fmt.Printf("%sall migration files are valid%s\n", colorGreen, colorReset)
	return nil
}

// MigrateCreateCmd scaffolds a new migration file.
type MigrateCreateCmd struct {
	// Name is snake-cased by goose with the next version prefix.
	Name string `arg:"" help:"Migration name, e.g. add_users_table"`
}

// Run scaffolds into the on-disk migrations directory.
func (c *MigrateCreateCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	path, createErr := database.CreateMigration(cfg.Database.URL, c.Name)
	if createErr != nil {
		return fmt.Errorf("db migrate:create: %w", createErr)
	}
	fmt.Printf("%screated%s %s\n", colorGreen, colorReset, path)
	return nil
}

// MigrateResetCmd rolls the schema back to its initial state.
type MigrateResetCmd struct {
	Force  bool `help:"Skip the confirmation prompt"`
	DryRun bool `help:"Print what would be rolled back without changing anything"`
}

// Run rolls back every migration after confirmation.
func (c *MigrateResetCmd) Run(cli *CLI) error {
	cfg, err := migrateConfig(cli)
	if err != nil {
		return err
	}
	ctx, cancel := dbContext()
	defer cancel()
	dsn := cfg.Database.URL

	if c.DryRun {
		return migrateResetDryRun(ctx, dsn)
	}

	if confirmErr := confirmDestructive(c.Force); confirmErr != nil {
		return confirmErr
	}
	rolled, resetErr := database.MigrateReset(ctx, dsn)
	if resetErr != nil {
		return fmt.Errorf("db migrate:reset: %w", resetErr)
	}
	for _, outcome := range rolled {
		fmt.Printf("%srolled back%s %s (%s)\n", colorGreen, colorReset, outcome.Path, outcome.Duration)
	}
	return nil
}

// migrateResetDryRun lists what reset would roll back, read-only.
func migrateResetDryRun(ctx context.Context, dsn string) error {
	statuses, err := database.MigrateStatus(ctx, dsn)
	if err != nil {
		return fmt.Errorf("db migrate:reset: %w", err)
	}
	var applied []database.MigrationStatus
	for _, entry := range statuses {
		if entry.State == "applied" {
			applied = append(applied, entry)
		}
	}
	if len(applied) == 0 {
		fmt.Printf("%snothing to roll back%s\n", colorCyan, colorReset)
		return nil
	}
	fmt.Printf("%sdry-run%s would roll back %d migration(s):\n", colorCyan, colorReset, len(applied))
	for _, entry := range applied {
		fmt.Printf("  %s\n", entry.Path)
	}
	return nil
}
