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

// printApplied reports the migrations that ran, one line each.
func printApplied(w io.Writer, results []database.Migration) error {
	for _, result := range results {
		state := "applied"
		if result.Empty {
			state = "empty"
		}
		if _, err := fmt.Fprintf(w, "%s %s (%s)\n", result.Name, state, result.Duration.Round(1_000_000)); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(w, "\n%d migration(s) applied\n", len(results))
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

// confirmApply reports whether the pending migrations may be applied. A
// non-interactive run applies without asking so that CI and `task db:migrate`
// never block; an interactive run asks unless --force is passed.
func confirmApply(cmd *cli.Command, interactive bool, pending int) (bool, error) {
	if cmd.Bool("force") || !interactive {
		return true, nil
	}

	out := cmd.Root().Writer
	if _, err := fmt.Fprintf(out, "apply %d pending migration(s)? [y/N] ", pending); err != nil {
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

	apply, err := confirmApply(cmd, isTerminal(cmd), len(selected))
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
	return printApplied(out, results)
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
