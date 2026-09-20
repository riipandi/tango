//go:build debug

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
)

var migrationName string

var migrateCreateCmd = &cli.Command{
	Name:     "migrate:create",
	Category: "Development commands",
	Usage:    "Create a new migration file",
	Arguments: []cli.Argument{
		&cli.StringArg{
			Name:        "name",
			UsageText:   "<MIGRATION_NAME>",
			Destination: &migrationName,
			Required:    true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Printf("Creating migration file: %s\n", migrationName)
		return nil
	},
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
// them again. --dry-run lists both halves without touching the database.
func runMigrateReset(ctx context.Context, cmd *cli.Command) error {
	migrator, closeDB, err := openMigrator(ctx, cmd, database.MigratorOptions{})
	if err != nil {
		return err
	}
	defer closeDB()

	out := cmd.Root().Writer

	applied, err := migrator.Applied(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		_, writeErr := fmt.Fprintln(out, "no applied migrations")
		return writeErr
	}

	reapply := cmd.Bool("up")
	if cmd.Bool("dry-run") {
		if err := printRollback(out, applied); err != nil {
			return err
		}
		if !reapply {
			return nil
		}
		// Every migration comes back, so the pending list is the full set.
		pending, err := migrator.Pending(ctx)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		return printPending(out, pending)
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
	reapplied, err := migrator.Up(ctx)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintln(out); err != nil {
		return err
	}
	return printResults(out, reapplied, "applied")
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
