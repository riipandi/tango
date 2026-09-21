package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/urfave/cli/v3"
)

// backupDir is where a dump lands when --output is not given. It sits inside the
// data directory, which git already ignores and compose already mounts.
const backupDir = "backup"

// backupFilePattern names a generated dump. Minute granularity keeps the name
// readable, and UTC keeps it consistent with every other timestamp the CLI
// prints.
const backupFilePattern = "tango-20060102_1504.sql"

var dbExportCmd = &cli.Command{
	Name:     "db:export",
	Category: "Database operation",
	Usage:    "Export the application database to a SQL file",
	Description: `Writes a plain SQL dump: the schema as DDL, then the rows as one COPY
block per table. The file is text, so it can be read, diffed, and
reviewed.

The default output is storage/backup/tango-<YYYYMMDD_hhmm>.sql, in
UTC. Pass --output to write somewhere else. A generated name that
already exists is not replaced unless --overwrite is given, so a
second export inside the same minute cannot lose the first dump.

db:import reads the file back. It loads the COPY blocks and ignores
the DDL, so the target database gets its schema from migrate:up.
That keeps the migrations the single source of truth for the schema.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "output",
			Usage: "Write the dump to this path",
		},
		&cli.BoolFlag{
			Name:  "schema-only",
			Usage: "Export the schema without any rows",
		},
		&cli.BoolFlag{
			Name:  "data-only",
			Usage: "Export the rows without any DDL",
		},
		&cli.BoolFlag{
			Name:  "overwrite",
			Usage: "Replace the generated dump file when it already exists",
		},
	},
	Action: runDBExport,
}

var dbImportCmd = &cli.Command{
	Name:      "db:import",
	Category:  "Database operation",
	Usage:     "Import a database backup file",
	ArgsUsage: "<file>",
	Description: `Loads the COPY blocks of a dump written by db:export into the
application database. DDL in the file is ignored: run migrate:up
first, so the schema comes from the migrations and nothing else.

The whole load runs in one transaction. A failure half way leaves
the database untouched.`,
	Arguments: []cli.Argument{
		&cli.StringArg{
			Name:      "file",
			UsageText: "backup file to load",
			Required:  true,
		},
	},
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:  "truncate",
			Usage: "Empty every table in the dump before loading it",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: runDBImport,
}

// defaultExportPath is where a dump is written when --output is not given.
func defaultExportPath(cmd *cli.Command) string {
	stamp := time.Now().UTC().Format(backupFilePattern)
	return filepath.Join(cmd.String("data-dir"), backupDir, stamp)
}

// openStore opens the pool a dump or a restore works through. The caller owns
// the handle and must close it.
func openStore(ctx context.Context, cmd *cli.Command) (*datastore.Postgres, string, error) {
	dsn, err := databaseURL(cmd)
	if err != nil {
		return nil, "", err
	}
	store, err := datastore.NewPostgres(ctx, datastore.PostgresOptions{DSN: dsn})
	if err != nil {
		return nil, "", err
	}
	return store, dsn, nil
}

// runDBExport writes a SQL dump of the application schemas.
//
// The dump is a plain SQL file: DDL first, then one COPY block per table. It is
// meant to be read, diffed, and reviewed, which is why the format is not a
// binary archive.
func runDBExport(ctx context.Context, cmd *cli.Command) error {
	out := cmd.Root().Writer

	opts := database.DumpOptions{
		SchemaOnly: cmd.Bool("schema-only"),
		DataOnly:   cmd.Bool("data-only"),
	}
	if opts.SchemaOnly && opts.DataOnly {
		return fmt.Errorf("--schema-only and --data-only are mutually exclusive")
	}

	store, dsn, err := openStore(ctx, cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err = printDatabase(out, dsn); err != nil {
		return err
	}

	path, generated := cmd.String("output"), false
	if path == "" {
		path, generated = defaultExportPath(cmd), true
	}

	file, err := createExportFile(path, generated, cmd.Bool("overwrite"))
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	started := time.Now()
	progress := withSpinner(out)
	stats, err := database.NewExporter(store, opts).Dump(ctx, file, progress.step)
	progress.finish()
	if err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("database: write dump: %w", err)
	}

	return printExportSummary(out, path, stats, time.Since(started))
}

// ErrDumpFileExists is returned when a dump would overwrite an existing file.
var ErrDumpFileExists = errors.New("database: dump file already exists")

// createExportFile opens the dump for writing.
//
// A generated path gets its directory created, because the default location
// lives under the data directory and that may not exist on a fresh checkout. The
// name carries a timestamp to the minute, so a second export within the same
// minute lands on the same path. Rather than lose the first dump, the run stops
// and says so, unless --overwrite asks for it.
//
// A path the user typed is used as it is: it is truncated like any other file
// the user named, and its directory is not created, so a typo stays visible.
func createExportFile(path string, generated, force bool) (*os.File, error) {
	if !generated || force {
		file, err := os.Create(path)
		if err != nil {
			return nil, fmt.Errorf("database: create dump file: %w", err)
		}
		return file, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("database: create backup directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return nil, fmt.Errorf("%w: %s; pass --output to pick another name, or --overwrite to replace it",
			ErrDumpFileExists, path)
	}
	if err != nil {
		return nil, fmt.Errorf("database: create dump file: %w", err)
	}
	return file, nil
}

// printExportSummary reports what was written and where, as a small aligned
// block. One labelled line per fact beats a sentence: a reader scans the labels
// instead of parsing prose, and a value can be read without the words around it.
func printExportSummary(out writer, path string, stats database.DumpStats, elapsed time.Duration) error {
	fields := []field{{"written", path}}
	if stats.Statements > 0 {
		fields = append(fields, field{"schema", fmt.Sprintf("%d %s",
			stats.Statements, plural(stats.Statements, "statement"))})
	}
	if stats.Tables > 0 {
		fields = append(fields, field{"data", fmt.Sprintf("%d %s, %d %s",
			stats.Tables, plural(stats.Tables, "table"),
			stats.Rows, plural(int(stats.Rows), "row"))})
	}
	fields = append(fields, field{"duration", humanDuration(elapsed)})
	return printFields(out, fields)
}

// printImportSummary reports what was loaded and from where.
func printImportSummary(out writer, path string, stats database.DumpStats, elapsed time.Duration) error {
	return printFields(out, []field{
		{"loaded", path},
		{"data", fmt.Sprintf("%d %s, %d %s",
			stats.Tables, plural(stats.Tables, "table"),
			stats.Rows, plural(int(stats.Rows), "row"))},
		{"duration", humanDuration(elapsed)},
	})
}

// field is one labelled value in a report.
type field struct {
	label string
	value string
}

// reportLabelWidth is the minimum width every label in a report block is padded
// to. A block can be printed in two parts — the target before the work, the
// result after it — and the two calls must agree on the column, so the width is
// pinned rather than derived from whichever fields a call happens to hold.
const reportLabelWidth = len("database")

// printFields writes labelled values with their labels padded to one width, so
// the values line up in a column and the block scans vertically.
func printFields(out writer, fields []field) error {
	width := reportLabelWidth
	for _, f := range fields {
		width = max(width, len(f.label))
	}
	for _, f := range fields {
		if _, err := fmt.Fprintf(out, "%-*s  %s\n", width+1, f.label+":", f.value); err != nil {
			return err
		}
	}
	return nil
}

// printDatabase writes the database a command works on as the opening line of
// its report block.
//
// It is printed before any work starts, so a run against the wrong server is
// visible immediately instead of after the fact. The credentials are never
// printed, and a DSN that cannot be parsed prints no line at all.
func printDatabase(out writer, dsn string) error {
	target := postgresTarget(dsn)
	if target == "" {
		return nil
	}
	return printFields(out, []field{{label: "database", value: target}})
}

// writer is the output surface a report needs, satisfied by the command writer.
type writer interface {
	Write(p []byte) (int, error)
}

// runDBImport loads a dump into the application database.
//
// The file is a required positional argument rather than a flag: a restore always
// has a source, and naming it on the command line is what a reader expects.
func runDBImport(ctx context.Context, cmd *cli.Command) error {
	out := cmd.Root().Writer
	path := cmd.StringArg("file")

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("database: open backup file: %w", err)
	}
	defer func() { _ = file.Close() }()

	store, dsn, err := openStore(ctx, cmd)
	if err != nil {
		return err
	}
	defer store.Close()

	if err = printDatabase(out, dsn); err != nil {
		return err
	}

	// Emptying a table is destructive, so it is confirmed like every other
	// destructive command. --force skips the prompt, which is what a script
	// wants.
	truncate := cmd.Bool("truncate")
	if truncate && !cmd.Bool("force") {
		proceed, promptErr := confirm(cmd, terminalCheck(cmd),
			"empty every table in the dump before loading it?")
		if promptErr != nil {
			return promptErr
		}
		if !proceed {
			_, writeErr := fmt.Fprintln(out, "import cancelled")
			return writeErr
		}
	}

	started := time.Now()
	progress := withSpinner(out)
	stats, err := database.NewRestorer(store, truncate).Restore(ctx, file, progress.step)
	progress.finish()
	if err != nil {
		return err
	}

	return printImportSummary(out, path, stats, time.Since(started))
}
