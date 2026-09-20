//go:build debug

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
)

var migrateCreateCmd = &cli.Command{
	Name:     "migrate:create",
	Category: "Development commands",
	Usage:    "Create a new migration file",
	Description: `Writes a new migration skeleton with the next free version and a
normalized name. The name is refused when another migration already
uses it, whatever its version, so no two files can share a name.

The file starts with empty Up and Down blocks; migrate:up reports such
a migration as "empty" until statements are added. A new file only
reaches the migrator after a rebuild, because migrations are embedded
in the binary.`,
	Arguments: []cli.Argument{
		&cli.StringArg{
			Name:      "name",
			UsageText: "<MIGRATION_NAME>",
			Required:  true,
		},
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "dir",
			Usage: "Directory to write into",
			Value: database.MigrationsPath,
		},
	},
	Action: runMigrateCreate,
}

// runMigrateCreate writes one migration file and reports where it landed.
func runMigrateCreate(_ context.Context, cmd *cli.Command) error {
	created, err := database.CreateMigration(database.CreateOptions{
		Dir:  cmd.String("dir"),
		Name: cmd.StringArg("name"),
	})
	if err != nil {
		return err
	}

	_, err = fmt.Fprintf(cmd.Root().Writer, "%s created (version %0*d)\n",
		created.Path, database.MigrationPrefixWidth, created.Version)
	return err
}

var migrateResetCmd = &cli.Command{
	Name:     "migrate:reset",
	Category: "Development commands",
	Usage:    "Rollback all migrations",
	Description: `Rolls back every applied migration, newest first. Pass --up to
re-apply them afterwards, which rebuilds the schema from scratch.`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "up",
			Usage: "Re-apply all migrations after the rollback (fresh schema)",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print what would be rolled back without changing anything",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: runMigrateReset,
}

var migrateSeedCmd = &cli.Command{
	Name:     "migrate:seed",
	Category: "Development commands",
	Usage:    "Seed the database with initial data",
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print what would be seeded without changing anything",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

// runMigrateReset rolls back every applied migration and, with --up, applies
// them again. On a database with nothing applied, --up alone applies the
// migrations, so resetting a fresh database still builds the schema.
// --dry-run lists both halves without touching the database.
func runMigrateReset(ctx context.Context, cmd *cli.Command) error {
	migrator, closeDB, err := openMigrator(ctx, cmd, database.MigratorOptions{})
	if err != nil {
		return err
	}
	defer closeDB()

	out := cmd.Root().Writer
	reapply := cmd.Bool("up")

	applied, err := migrator.Applied(ctx)
	if err != nil {
		return err
	}

	// Nothing to roll back. Without --up that is the whole answer, because
	// there is no up half to run either.
	if len(applied) == 0 && !reapply {
		_, writeErr := fmt.Fprintln(out, "no applied migrations")
		return writeErr
	}

	if cmd.Bool("dry-run") {
		return planReset(ctx, migrator, out, applied, reapply)
	}

	// A fresh database has no rollback to do, so --up is a plain apply.
	if len(applied) == 0 {
		return applyResetUp(ctx, cmd, migrator, out)
	}

	question := fmt.Sprintf("roll back all %d migration(s)?", len(applied))
	if reapply {
		question = fmt.Sprintf("roll back all %d migration(s) and re-apply them?", len(applied))
	}
	proceed, err := confirm(cmd, terminalCheck(cmd), question)
	if err != nil {
		return err
	}
	if !proceed {
		_, writeErr := fmt.Fprintf(out, "\n%d migration(s) left applied\n", len(applied))
		return writeErr
	}

	rolled, err := migrator.Down(ctx, len(applied))
	if err != nil {
		if len(rolled) > 0 {
			if writeErr := printResults(out, rolled, "rolled back"); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	if err := printResults(out, rolled, "rolled back"); err != nil {
		return err
	}

	if !reapply {
		return nil
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	reapplied, err := migrator.Up(ctx)
	if err != nil {
		return err
	}
	return printResults(out, reapplied, "applied")
}

// applyResetUp runs the --up half on a database with nothing applied. It asks
// the same question migrate:up asks, because it does the same work.
func applyResetUp(
	ctx context.Context,
	cmd *cli.Command,
	migrator *database.Migrator,
	out io.Writer,
) error {
	pending, err := migrator.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		_, writeErr := fmt.Fprintln(out, "no pending migrations")
		return writeErr
	}

	proceed, err := confirm(cmd, terminalCheck(cmd),
		fmt.Sprintf("apply all %d pending migration(s)?", len(pending)))
	if err != nil {
		return err
	}
	if !proceed {
		_, writeErr := fmt.Fprintf(out, "\n%d pending migration(s) left unapplied\n", len(pending))
		return writeErr
	}

	results, err := migrator.Up(ctx)
	if err != nil {
		return err
	}
	return printResults(out, results, "applied")
}

// planReset prints both halves of a reset without touching the database. The
// rollback half is skipped when the database has nothing applied.
func planReset(
	ctx context.Context,
	migrator *database.Migrator,
	out io.Writer,
	applied []database.MigrationStatus,
	reapply bool,
) error {
	if len(applied) > 0 {
		if err := printRollback(out, applied); err != nil {
			return err
		}
	}
	if !reapply {
		return nil
	}
	if len(applied) > 0 {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
	}

	pending, err := migrator.Pending(ctx)
	if err != nil {
		return err
	}
	return printPending(out, pending)
}

// runMigrateValidate checks the embedded migrations and reports every problem.
// It never connects to a database, so it works on a machine without Postgres.
func runMigrateValidate(_ context.Context, cmd *cli.Command) error {
	return printValidation(cmd.Root().Writer, migrationCheck())
}

// migrationCheck is the seam the tests replace to exercise the failing path,
// which the embedded migrations cannot produce.
var migrationCheck = database.Validate

// printValidation reports the outcome and returns an error when the migrations
// are not valid, so the process exits non-zero in CI.
func printValidation(w io.Writer, report database.ValidationReport) error {
	for _, issue := range report.Issues {
		if _, err := fmt.Fprintf(w, "%s\n", issue); err != nil {
			return err
		}
	}
	if !report.OK() {
		return fmt.Errorf("database: %d problem(s) in %d migration file(s)",
			len(report.Issues), report.Checked)
	}

	_, err := fmt.Fprintf(w, "%d migration file(s) valid\n", report.Checked)
	return err
}

var migrateValidateCmd = &cli.Command{
	Name:     "migrate:validate",
	Category: "Development commands",
	Usage:    "Check the migration files",
	Description: `Checks the migrations embedded in this binary for the mistakes goose
rejects while applying: unparsable names, duplicate or missing
versions, malformed annotations, and files without a Down block.

The check reads nothing outside the binary and needs no database, so
it runs before a connection exists. It does not parse SQL.`,
	Action: runMigrateValidate,
}
