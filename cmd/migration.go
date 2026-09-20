package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/urfave/cli/v3"
	"golang.org/x/term"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/envfile"
)

// ErrDatabaseURLUnset is returned when no DSN is available.
var ErrDatabaseURLUnset = fmt.Errorf("%s is not set; pass --env-file or export it", envfile.DatabaseURL)

// databaseURL resolves the DSN. The file named by the root --env-file wins over
// the process environment, matching the documented precedence.
func databaseURL(cmd *cli.Command) (string, error) {
	if path := cmd.String("env-file"); path != "" {
		file, err := envfile.Load(path)
		if err != nil {
			return "", err
		}
		if dsn, ok := file.Get(envfile.DatabaseURL); ok && dsn != "" {
			return dsn, nil
		}
	}
	if dsn := os.Getenv(envfile.DatabaseURL); dsn != "" {
		return dsn, nil
	}
	return "", ErrDatabaseURLUnset
}

// openMigrator opens the single-connection migration handle and loads the
// migrations embedded in the binary. The returned close function releases the
// handle and is always non-nil on success.
func openMigrator(
	ctx context.Context,
	cmd *cli.Command,
	opts database.MigratorOptions,
) (*database.Migrator, func(), error) {
	dsn, err := databaseURL(cmd)
	if err != nil {
		return nil, nil, err
	}

	db, err := datastore.OpenMigrationDB(ctx, datastore.PostgresOptions{DSN: dsn})
	if err != nil {
		return nil, nil, err
	}

	migrator, err := database.NewMigrator(ctx, db, opts)
	if err != nil {
		_ = db.Close()
		return nil, nil, err
	}
	return migrator, func() { _ = db.Close() }, nil
}

// printResults reports the migrations that ran, one line each. verb describes
// the direction: "applied" or "rolled back".
func printResults(w io.Writer, results []database.Migration, verb string) error {
	for _, result := range results {
		state := verb
		if result.Empty {
			state = "empty"
		}
		if _, err := fmt.Fprintf(w, "%s %s (%s)\n", result.Name, state, result.Duration.Round(1_000_000)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%d migration(s) %s\n", len(results), verb)
	return err
}

// printPending reports the migrations the database has not applied yet.
func printPending(w io.Writer, pending []database.MigrationStatus) error {
	if len(pending) == 0 {
		_, err := fmt.Fprintln(w, "no pending migrations")
		return err
	}
	for _, status := range pending {
		if _, err := fmt.Fprintf(w, "%05d %s\n", status.Version, status.Name); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%d pending migration(s)\n", len(pending))
	return err
}

// printRollback reports the migrations a rollback would consume, newest first.
func printRollback(w io.Writer, selected []database.MigrationStatus) error {
	for _, status := range selected {
		if _, err := fmt.Fprintf(w, "%05d %s\n", status.Version, status.Name); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%d migration(s) to roll back\n", len(selected))
	return err
}

// printStatus reports every embedded migration and the database version, which
// is the highest applied migration.
func printStatus(w io.Writer, statuses []database.MigrationStatus, version int64) error {
	applied := 0
	for _, status := range statuses {
		state := "pending"
		if status.Applied {
			state = "applied"
			applied++
		}
		if _, err := fmt.Fprintf(w, "%05d %-7s %s\n", status.Version, state, status.Name); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\nversion %05d; %d of %d applied\n", version, applied, len(statuses))
	return err
}

// confirm asks the question and reports whether the user agreed. A
// non-interactive run agrees without asking so that CI and `task db:migrate`
// never block; an interactive run asks unless --force is passed.
func confirm(cmd *cli.Command, interactive bool, question string) (bool, error) {
	if cmd.Bool("force") || !interactive {
		return true, nil
	}

	out := cmd.Root().Writer
	if _, err := fmt.Fprintf(out, "%s [y/N] ", question); err != nil {
		return false, err
	}

	in := cmd.Root().Reader
	if in == nil {
		in = os.Stdin
	}
	answer, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && answer == "" {
		if _, writeErr := fmt.Fprintln(out); writeErr != nil {
			return false, writeErr
		}
		return false, nil
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true, nil
	default:
		return false, nil
	}
}

// runMigrateUp applies the pending migrations. --dry-run lists them instead and
// changes nothing.
func runMigrateUp(ctx context.Context, cmd *cli.Command) error {
	migrator, closeDB, err := openMigrator(ctx, cmd, database.MigratorOptions{})
	if err != nil {
		return err
	}
	defer closeDB()

	out := cmd.Root().Writer

	pending, err := migrator.Pending(ctx)
	if err != nil {
		return err
	}
	if cmd.Bool("dry-run") {
		return printPending(out, pending)
	}

	// --to caps what this run applies; migration version 0 is goose's sentinel,
	// never a target, so an unset --to means "everything".
	target := migrator.HighestVersion()
	if requested := cmd.Uint64("to"); requested > 0 {
		if requested > math.MaxInt64 {
			return fmt.Errorf("database: --to=%d exceeds the maximum migration version", requested)
		}
		target = int64(requested)
	}
	selected := pendingUpTo(pending, target)

	if len(selected) == 0 {
		return printNothingToApply(out, pending, target)
	}

	apply, err := confirm(cmd, terminalCheck(cmd),
		fmt.Sprintf("apply %d pending migration(s)?", len(selected)))
	if err != nil {
		return err
	}
	if !apply {
		_, writeErr := fmt.Fprintf(out, "\n%d pending migration(s) left unapplied\n", len(selected))
		return writeErr
	}

	results, err := migrator.UpTo(ctx, target)
	if err != nil {
		return err
	}
	return printResults(out, results, "applied")
}

// runMigrateDown rolls back the most recent migrations. --dry-run lists them
// instead and changes nothing.
func runMigrateDown(ctx context.Context, cmd *cli.Command) error {
	count := cmd.Int("count")
	if count <= 0 {
		return fmt.Errorf("database: --count must be greater than zero, got %d", count)
	}

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

	// A count above what the database has rolls back everything, which is the
	// only sensible reading.
	count = min(count, len(applied))
	if cmd.Bool("dry-run") {
		return printRollback(out, applied[:count])
	}

	proceed, err := confirm(cmd, terminalCheck(cmd),
		fmt.Sprintf("roll back %d migration(s)?", count))
	if err != nil {
		return err
	}
	if !proceed {
		_, writeErr := fmt.Fprintf(out, "\n%d migration(s) left applied\n", count)
		return writeErr
	}

	results, err := migrator.Down(ctx, count)
	if err != nil {
		// Some migrations may have rolled back before the failure; report them
		// before surfacing the error.
		if len(results) > 0 {
			if writeErr := printResults(out, results, "rolled back"); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	return printResults(out, results, "rolled back")
}

// runMigrateStatus lists every embedded migration and whether the database has
// it.
func runMigrateStatus(ctx context.Context, cmd *cli.Command) error {
	migrator, closeDB, err := openMigrator(ctx, cmd, database.MigratorOptions{})
	if err != nil {
		return err
	}
	defer closeDB()

	statuses, err := migrator.Status(ctx)
	if err != nil {
		return err
	}
	version, err := migrator.Version(ctx)
	if err != nil {
		return err
	}
	return printStatus(cmd.Root().Writer, statuses, version)
}

// runMigrateVersion prints the version the database sits on, which is the
// highest applied migration. A database that has never been migrated reports 0.
func runMigrateVersion(ctx context.Context, cmd *cli.Command) error {
	migrator, closeDB, err := openMigrator(ctx, cmd, database.MigratorOptions{})
	if err != nil {
		return err
	}
	defer closeDB()

	version, err := migrator.Version(ctx)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(cmd.Root().Writer, version)
	return err
}

// pendingUpTo keeps the migrations that a run targeting version would apply.
func pendingUpTo(pending []database.MigrationStatus, version int64) []database.MigrationStatus {
	selected := make([]database.MigrationStatus, 0, len(pending))
	for _, status := range pending {
		if status.Version <= version {
			selected = append(selected, status)
		}
	}
	return selected
}

// printNothingToApply explains why a run applied nothing. An empty result under
// --to is not the same as an up-to-date database, and saying "no pending
// migrations" then would be wrong.
func printNothingToApply(w io.Writer, pending []database.MigrationStatus, target int64) error {
	if len(pending) == 0 {
		_, err := fmt.Fprintln(w, "no pending migrations")
		return err
	}
	_, err := fmt.Fprintf(w,
		"nothing to apply up to version %05d; %d migration(s) pending above it\n", target, len(pending))
	return err
}

// isTerminal reports whether the confirmation prompt has a user behind it.
// os.ModeCharDevice is not enough: /dev/null is a character device too, and a
// piped run must never block waiting for an answer.
func isTerminal(cmd *cli.Command) bool {
	in := cmd.Root().Reader
	if in == nil {
		in = os.Stdin
	}
	file, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// terminalCheck is the seam the tests replace to exercise both branches of the
// confirmation prompt, which otherwise needs a real terminal.
var terminalCheck = isTerminal
