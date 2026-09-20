//go:build debug

package main

import (
	"context"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/database/seeders"
	"github.com/riipandi/tango/internal/datastore"
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
	Description: `Creates the default records a fresh database needs. Every seeder is
idempotent, so running this command twice changes nothing the second
time: an existing record is reported as skipped.

Seeding writes data, so it asks for confirmation. It runs in one
transaction, so a seeder that fails leaves nothing behind. --dry-run
reports what would be created without writing anything.

The database must be migrated first; run migrate:up.`,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Report what would be seeded without changing anything",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: runMigrateSeed,
}

// runMigrateSeed applies every seeder and reports what each one created.
func runMigrateSeed(ctx context.Context, cmd *cli.Command) error {
	dsn, err := databaseURL(cmd)
	if err != nil {
		return err
	}

	if err := requireMigrated(ctx, dsn); err != nil {
		return err
	}

	pool, err := datastore.NewPostgres(ctx, datastore.PostgresOptions{DSN: dsn})
	if err != nil {
		return err
	}
	defer pool.Close()

	out := cmd.Root().Writer
	dryRun := cmd.Bool("dry-run")

	// A dry run writes nothing, so it needs no confirmation and no
	// transaction: there is nothing to roll back.
	if dryRun {
		results, err := seeders.Run(ctx, pool, true, seeders.All()...)
		if err != nil {
			return err
		}
		return printSeedResults(out, results, true)
	}

	proceed, err := confirm(cmd, terminalCheck(cmd), "seed the database?")
	if err != nil {
		return err
	}
	if !proceed {
		_, writeErr := fmt.Fprintln(out, "\nnothing seeded")
		return writeErr
	}

	var results []seeders.Result
	err = pool.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var seedErr error
		results, seedErr = seeders.Run(ctx, tx, false, seeders.All()...)
		return seedErr
	})
	if err != nil {
		return err
	}
	return printSeedResults(out, results, false)
}

// requireMigrated refuses to seed a database whose schema is not current.
//
// A seeder writes the columns it knows about, so a missing table or a table
// from an older migration would fail halfway through with a database error that
// does not say what to do. Checking the migration state first turns that into
// one instruction, and it catches the case a table check misses: a database
// rolled back below the version a seeder needs.
//
// The check opens the single-connection migration handle rather than reusing
// the pool, because goose reads its version table through database/sql. Asking
// goose is deliberate: "pending" is the engine's own answer, so the check
// cannot drift from what migrate:up would do.
func requireMigrated(ctx context.Context, dsn string) error {
	db, err := datastore.OpenMigrationDB(ctx, datastore.PostgresOptions{DSN: dsn})
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	migrator, err := database.NewMigrator(ctx, db, database.MigratorOptions{})
	if err != nil {
		return err
	}

	pending, err := migrator.Pending(ctx)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		return fmt.Errorf("database: %d migration(s) pending; run migrate:up first", len(pending))
	}
	return nil
}

// printSeedResults reports one line per record and a summary. A dry run uses
// future tense, so its output cannot be mistaken for a report of work done.
func printSeedResults(w io.Writer, results []seeders.Result, dryRun bool) error {
	var created, skipped int
	for _, result := range results {
		for _, key := range result.Created {
			created++
			if err := printSeedLine(w, result.Name, key, "created", "would create", dryRun); err != nil {
				return err
			}
		}
		for _, key := range result.Skipped {
			skipped++
			if err := printSeedLine(w, result.Name, key, "skipped", "would skip", dryRun); err != nil {
				return err
			}
		}
	}

	if created+skipped > 0 {
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if dryRun {
		_, err := fmt.Fprintf(w, "%d to create, %d to skip\n", created, skipped)
		return err
	}
	_, err := fmt.Fprintf(w, "%d created, %d skipped\n", created, skipped)
	return err
}

// printSeedLine writes one record. The seeder name comes first so the output
// sorts and greps by what was seeded. Both wordings are passed in rather than
// derived: "create" and "skip" do not share a past-tense rule.
func printSeedLine(w io.Writer, seeder, key, verb, dryRunVerb string, dryRun bool) error {
	if dryRun {
		verb = dryRunVerb
	}
	_, err := fmt.Fprintf(w, "%s %s %s\n", seeder, key, verb)
	return err
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
