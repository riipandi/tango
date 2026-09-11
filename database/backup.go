// Backup and restore tooling ported from scripts/migrator.sh: a
// thin, typed wrapper around the PostgreSQL client binaries.
//
//	dump     — pg_dump, binary custom format (fast, smaller)
//	export   — pg_dump, plain SQL (portable, editable)
//	restore  — pg_restore from a custom-format dump
//	import   — psql from a plain SQL file
//
// The DSN is parsed with pgx itself — no shell string splitting.
// restore and import replace database content and are therefore
// gated by the caller (confirm/--force/--dry-run in the launcher).
package database

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// backupDir receives every dump and export, relative to the
	// working directory of the invoker.
	backupDir = "storage/backup"

	// timestampLayout names backup files without spaces or colons.
	timestampLayout = "20060102_150405"

	// systemSchemaExcludes keeps PostgreSQL internals out of the
	// dumps.
	systemSchemaExcludes = "-N information_schema -N pg_catalog -N pg_toast"
)

// pgTool resolves a PostgreSQL client binary from PATH.
func pgTool(name string) (string, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s not found in PATH; install the PostgreSQL client tools", name)
	}
	return path, nil
}

// connParts holds the parsed DSN fields the client binaries need.
type connParts struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// parseDSN reuses the pgx parser so there is exactly one place that
// understands connection strings.
func parseDSN(dsn string) (connParts, error) {
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		return connParts{}, fmt.Errorf("parse DSN: %w", err)
	}
	parts := connParts{
		Host:     cfg.Host,
		Port:     strconv.Itoa(int(cfg.Port)),
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
	}
	if parts.Host == "" {
		parts.Host = "localhost"
	}
	return parts, nil
}

// baseArgs builds the shared -U/-h/-p flags; the password travels
// through the command environment (never argv).
func (c connParts) baseArgs() []string {
	return []string{"-U", c.User, "-h", c.Host, "-p", c.Port, "-d", c.Database}
}

// env returns the command environment with the password set.
func (c connParts) env() []string {
	if c.Password == "" {
		return os.Environ()
	}
	return append(os.Environ(), "PGPASSWORD="+c.Password)
}

// run executes a client binary against the database, mirroring the
// caller's cancellation and streaming output to the process's own.
func runTool(ctx context.Context, parts connParts, name string, args ...string) error {
	bin, err := pgTool(name)
	if err != nil {
		return err
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = parts.env()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

// backupPath returns the backup file path for a database and
// suffix, creating the backup directory.
func backupPath(database, suffix, ext string) (string, error) {
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	stamp := time.Now().Format(timestampLayout)
	return filepath.Join(backupDir, fmt.Sprintf("%s_%s_%s.%s", database, suffix, stamp, ext)), nil
}

// Dump creates a custom-format (binary) backup: all = schema and
// data, data = data only. Returns the created file path.
func Dump(ctx context.Context, dsn, mode string) (string, error) {
	if mode != "all" && mode != "data" {
		return "", fmt.Errorf("unknown dump mode %q (want all or data)", mode)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return "", err
	}
	suffix := "full"
	if mode == "data" {
		suffix = "data"
	}
	target, err := backupPath(parts.Database, suffix, "dump")
	if err != nil {
		return "", err
	}

	args := parts.baseArgs()
	if mode == "data" {
		args = append(args, "--data-only")
	}
	args = append(args,
		"-F", "c",
		"-f", target,
	)
	args = append(args, strings.Fields(systemSchemaExcludes)...)
	args = append(args, "--no-owner", "--no-acl")

	return target, runTool(ctx, parts, "pg_dump", args...)
}

// Export creates a plain-SQL backup: all = schema and data with
// clean statements, data = inserts only. Returns the created path.
func Export(ctx context.Context, dsn, mode string) (string, error) {
	if mode != "all" && mode != "data" {
		return "", fmt.Errorf("unknown export mode %q (want all or data)", mode)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return "", err
	}
	suffix := "full"
	if mode == "data" {
		suffix = "data"
	}
	target, err := backupPath(parts.Database, suffix, "sql")
	if err != nil {
		return "", err
	}

	args := parts.baseArgs()
	if mode == "data" {
		args = append(args, "--data-only", "--column-inserts")
	} else {
		args = append(args, "--clean", "--if-exists")
	}
	args = append(args,
		"-f", target,
	)
	args = append(args, strings.Fields(systemSchemaExcludes)...)
	args = append(args, "--no-owner", "--no-acl")

	return target, runTool(ctx, parts, "pg_dump", args...)
}

// Restore loads a custom-format dump back into the database:
// all = schema and data (--clean first), data = data only,
// schema = schema only. Destructive — the caller gates it.
func Restore(ctx context.Context, dsn, mode, dumpFile string) error {
	bin, parts, args, err := restoreArgs(dsn, mode, dumpFile)
	if err != nil {
		return err
	}
	return runToolWith(ctx, bin, parts, args...)
}

// RestoreCommand renders the exact pg_restore invocation Restore
// would run, for --dry-run.
func RestoreCommand(dsn, mode, dumpFile string) (string, error) {
	bin, parts, args, err := restoreArgs(dsn, mode, dumpFile)
	if err != nil {
		return "", err
	}
	return commandString(bin, parts, args), nil
}

// restoreArgs validates the request and builds the pg_restore
// arguments.
func restoreArgs(dsn, mode, dumpFile string) (string, connParts, []string, error) {
	if mode != "all" && mode != "data" && mode != "schema" {
		return "", connParts{}, nil, fmt.Errorf("unknown restore mode %q (want all, data, or schema)", mode)
	}
	if _, err := os.Stat(dumpFile); err != nil {
		return "", connParts{}, nil, fmt.Errorf("dump file: %w", err)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return "", connParts{}, nil, err
	}

	args := parts.baseArgs()
	switch mode {
	case "data":
		args = append(args, "--data-only", "--disable-triggers")
	case "schema":
		args = append(args, "--schema-only", "--clean", "--if-exists")
	default: // all
		args = append(args, "--clean", "--if-exists")
	}
	args = append(args, "--no-owner", "--no-acl")
	args = append(args, strings.Fields(systemSchemaExcludes)...)
	args = append(args, dumpFile)

	bin, err := pgTool("pg_restore")
	if err != nil {
		return "", connParts{}, nil, err
	}
	return bin, parts, args, nil
}

// Import runs a plain SQL file through psql. Like the script it
// ports, it continues past per-statement errors (ON_ERROR_STOP=off)
// so partial imports are visible. Destructive — the caller gates it.
func Import(ctx context.Context, dsn, sqlFile string) error {
	bin, parts, args, err := importArgs(dsn, sqlFile)
	if err != nil {
		return err
	}
	return runToolWith(ctx, bin, parts, args...)
}

// ImportCommand renders the exact psql invocation Import would
// run, for --dry-run.
func ImportCommand(dsn, sqlFile string) (string, error) {
	bin, parts, args, err := importArgs(dsn, sqlFile)
	if err != nil {
		return "", err
	}
	return commandString(bin, parts, args), nil
}

// importArgs validates the request and builds the psql arguments.
func importArgs(dsn, sqlFile string) (string, connParts, []string, error) {
	if _, err := os.Stat(sqlFile); err != nil {
		return "", connParts{}, nil, fmt.Errorf("sql file: %w", err)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return "", connParts{}, nil, err
	}

	args := parts.baseArgs()
	args = append(args,
		"-f", sqlFile,
		"--variable=ON_ERROR_STOP=off",
		"--quiet",
	)

	bin, err := pgTool("psql")
	if err != nil {
		return "", connParts{}, nil, err
	}
	return bin, parts, args, nil
}

// runToolWith executes a resolved client binary.
func runToolWith(ctx context.Context, bin string, parts connParts, args ...string) error {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = parts.env()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", filepath.Base(bin), err)
	}
	return nil
}

// commandString renders a command line for --dry-run output,
// hiding the password position (it travels via the environment).
func commandString(bin string, parts connParts, args []string) string {
	return fmt.Sprintf("%s %s", filepath.Base(bin), strings.Join(args, " "))
}
