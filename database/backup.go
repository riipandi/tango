// Package database wraps pg_dump, pg_restore, and psql.
//
//	dump:     pg_dump in custom format
//	export:   pg_dump in plain SQL
//	restore:  pg_restore from a dump file
//	import:   psql from a SQL file
//
// DSNs are parsed with pgx only. restore/import are destructive;
// callers gate them with confirm, --force, and --dry-run.
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

	"github.com/riipandi/tango/internal/config"
)

const (
	// Dump/export subdir under <data-dir>/backup.
	defaultBackupSubdir = "backup"

	// File timestamps without spaces or colons.
	timestampLayout = "20060102_150405"

	// Keeps PostgreSQL internals out of dumps.
	systemSchemaExcludes = "-N information_schema -N pg_catalog -N pg_toast"
)

// Executor runs PostgreSQL client commands. env carries the process
// environment (including PGPASSWORD, which must never appear in argv).
// The default implementation uses host binaries; tests can provide a
// container-backed implementation.
type Executor interface {
	PGDump(ctx context.Context, env []string, args []string, outputPath string) error
	PGRestore(ctx context.Context, env []string, args []string) error
	PSQL(ctx context.Context, env []string, args []string) error
}

// DefaultExecutor is the executor used by the package-level Dump,
// Export, Restore, and Import helpers. Tests override it.
var DefaultExecutor Executor = LocalExecutor{}

// LocalExecutor runs commands using host binaries.
type LocalExecutor struct{}

func (LocalExecutor) PGDump(ctx context.Context, env []string, args []string, outputPath string) error {
	return runTool(ctx, env, "pg_dump", append(args, "-f", outputPath)...)
}

func (LocalExecutor) PGRestore(ctx context.Context, env []string, args []string) error {
	return runTool(ctx, env, "pg_restore", args...)
}

func (LocalExecutor) PSQL(ctx context.Context, env []string, args []string) error {
	return runTool(ctx, env, "psql", args...)
}

// connParts holds parsed DSN fields for the client binaries.
type connParts struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

// parseDSN uses the pgx parser: one place understands DSNs.
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

// baseArgs builds shared -U/-h/-p flags; password goes via env,
// never argv.
func (c connParts) baseArgs() []string {
	return []string{"-U", c.User, "-h", c.Host, "-p", c.Port, "-d", c.Database}
}

// env adds the password to the command environment.
func (c connParts) env() []string {
	if c.Password == "" {
		return os.Environ()
	}
	return append(os.Environ(), "PGPASSWORD="+c.Password)
}

// runTool executes a client binary on the host. env must carry
// PGPASSWORD when the DSN has one; without it the binary prompts on
// the terminal and blocks the caller.
func runTool(ctx context.Context, env []string, name string, args ...string) error {
	bin, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("%s not found in PATH; install the PostgreSQL client tools", name)
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", name, err)
	}
	return nil
}

// backupDirFor resolves the backup dir: explicit wins (launcher
// passes cfg.BackupDir()), else a temp default for tests.
func backupDirFor(override string) string {
	if strings.TrimSpace(override) != "" {
		return override
	}
	return filepath.Join(os.TempDir(), config.AppName+"-"+defaultBackupSubdir)
}

// backupPathIn builds the file path inside dir, creating it.
// 0700: backups hold full application data.
func backupPathIn(dir, database, suffix, ext string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create backup dir: %w", err)
	}
	stamp := time.Now().Format(timestampLayout)
	return filepath.Join(dir, fmt.Sprintf("%s_%s_%s.%s", database, suffix, stamp, ext)), nil
}

// restrictBackupFile narrows a finished dump to owner-only access;
// pg_dump follows the process umask, which is usually world-readable.
func restrictBackupFile(target string) error {
	if err := os.Chmod(target, 0o600); err != nil {
		return fmt.Errorf("restrict backup file: %w", err)
	}
	return nil
}

// Dump writes a custom-format backup. all = schema+data,
// data = data only. Returns the file path.
func Dump(ctx context.Context, dsn, mode, dir string) (string, error) {
	return DumpWithExecutor(ctx, DefaultExecutor, dsn, mode, dir)
}

// DumpWithExecutor writes a custom-format backup using the provided executor.
func DumpWithExecutor(ctx context.Context, exec Executor, dsn, mode, dir string) (string, error) {
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
	target, err := backupPathIn(backupDirFor(dir), parts.Database, suffix, "dump")
	if err != nil {
		return "", err
	}

	args := parts.baseArgs()
	if mode == "data" {
		args = append(args, "--data-only")
	}
	args = append(args,
		"-F", "c",
	)
	args = append(args, strings.Fields(systemSchemaExcludes)...)
	args = append(args, "--no-owner", "--no-acl")

	if err := exec.PGDump(ctx, parts.env(), args, target); err != nil {
		return "", err
	}
	return target, restrictBackupFile(target)
}

// Export writes a plain-SQL backup. all = schema+data with clean
// statements, data = inserts only. Returns the file path.
func Export(ctx context.Context, dsn, mode, dir string) (string, error) {
	return ExportWithExecutor(ctx, DefaultExecutor, dsn, mode, dir)
}

// ExportWithExecutor writes a plain-SQL backup using the provided executor.
func ExportWithExecutor(ctx context.Context, exec Executor, dsn, mode, dir string) (string, error) {
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
	target, err := backupPathIn(backupDirFor(dir), parts.Database, suffix, "sql")
	if err != nil {
		return "", err
	}

	args := parts.baseArgs()
	if mode == "data" {
		args = append(args, "--data-only", "--column-inserts")
	} else {
		args = append(args, "--clean", "--if-exists")
	}
	args = append(args, strings.Fields(systemSchemaExcludes)...)
	args = append(args, "--no-owner", "--no-acl")

	if err := exec.PGDump(ctx, parts.env(), args, target); err != nil {
		return "", err
	}
	return target, restrictBackupFile(target)
}

// Restore loads a custom-format dump. all = schema+data (--clean),
// data = data only, schema = schema only. Destructive.
func Restore(ctx context.Context, dsn, mode, dumpFile string) error {
	return RestoreWithExecutor(ctx, DefaultExecutor, dsn, mode, dumpFile)
}

// RestoreWithExecutor loads a custom-format dump using the provided executor.
func RestoreWithExecutor(ctx context.Context, exec Executor, dsn, mode, dumpFile string) error {
	args, err := restoreArgs(dsn, mode, dumpFile)
	if err != nil {
		return err
	}
	parts, err := parseDSN(dsn)
	if err != nil {
		return err
	}
	return exec.PGRestore(ctx, parts.env(), args)
}

// RestoreCommand renders the pg_restore call for --dry-run.
func RestoreCommand(dsn, mode, dumpFile string) (string, error) {
	args, err := restoreArgs(dsn, mode, dumpFile)
	if err != nil {
		return "", err
	}
	return commandString("pg_restore", args), nil
}

// restoreArgs validates and builds pg_restore arguments.
func restoreArgs(dsn, mode, dumpFile string) ([]string, error) {
	if mode != "all" && mode != "data" && mode != "schema" {
		return nil, fmt.Errorf("unknown restore mode %q (want all, data, or schema)", mode)
	}
	if _, err := os.Stat(dumpFile); err != nil {
		return nil, fmt.Errorf("dump file: %w", err)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return nil, err
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

	return args, nil
}

// Import runs a SQL file with ON_ERROR_STOP=on: the first failure
// aborts, so partial imports never report success. Destructive.
func Import(ctx context.Context, dsn, sqlFile string) error {
	return ImportWithExecutor(ctx, DefaultExecutor, dsn, sqlFile)
}

// ImportWithExecutor runs a SQL file using the provided executor.
func ImportWithExecutor(ctx context.Context, exec Executor, dsn, sqlFile string) error {
	args, err := importArgs(dsn, sqlFile)
	if err != nil {
		return err
	}
	parts, err := parseDSN(dsn)
	if err != nil {
		return err
	}
	return exec.PSQL(ctx, parts.env(), args)
}

// ImportCommand renders the psql call for --dry-run.
func ImportCommand(dsn, sqlFile string) (string, error) {
	args, err := importArgs(dsn, sqlFile)
	if err != nil {
		return "", err
	}
	return commandString("psql", args), nil
}

// importArgs validates and builds psql arguments.
func importArgs(dsn, sqlFile string) ([]string, error) {
	if _, err := os.Stat(sqlFile); err != nil {
		return nil, fmt.Errorf("sql file: %w", err)
	}

	parts, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}

	args := parts.baseArgs()
	args = append(args,
		"-f", sqlFile,
		"--variable=ON_ERROR_STOP=on",
		"--quiet",
	)

	return args, nil
}

// commandString renders a --dry-run line; password stays in env.
func commandString(bin string, args []string) string {
	return fmt.Sprintf("%s %s", bin, strings.Join(args, " "))
}
