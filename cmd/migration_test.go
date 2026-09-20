package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/envfile"
	"github.com/riipandi/tango/pkg/testutils"
)

// writeEnvFile writes a dotenv file containing DATABASE_URL.
func writeEnvFile(t *testing.T, dsn string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env.local")
	require.NoError(t, os.WriteFile(path, []byte(envfile.DatabaseURL+"="+dsn+"\n"), 0o600))
	return path
}

// runMigrateCmd executes a migration command with args and returns stdout.
func runMigrateCmd(t *testing.T, cmd *cli.Command, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer

	root := &cli.Command{
		Name:     "tango",
		Writer:   &out,
		Reader:   strings.NewReader(stdin),
		Commands: []*cli.Command{cmd},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "env-file", Usage: "Load environment variables from a file"},
		},
	}
	err := root.Run(context.Background(), append([]string{"tango", cmd.Name}, args...))
	return out.String(), err
}

// runMigrateUpCmd executes migrate:up with args and returns stdout.
func runMigrateUpCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateUpCmd, stdin, args...)
}

func TestDatabaseURLPrefersEnvFileOverEnvironment(t *testing.T) {
	t.Setenv(envfile.DatabaseURL, "postgres://from-environment/db")

	path := writeEnvFile(t, "postgres://from-env-file/db")

	resolved, err := resolveDatabaseURL(t, path)
	require.NoError(t, err)
	assert.Equal(t, "postgres://from-env-file/db", resolved)
}

func TestDatabaseURLFallsBackToEnvironment(t *testing.T) {
	t.Setenv(envfile.DatabaseURL, "postgres://from-environment/db")

	resolved, err := resolveDatabaseURL(t, "")
	require.NoError(t, err)
	assert.Equal(t, "postgres://from-environment/db", resolved)
}

func TestDatabaseURLMissing(t *testing.T) {
	_, err := resolveDatabaseURL(t, "")
	require.ErrorIs(t, err, ErrDatabaseURLUnset)
}

// resolveDatabaseURL runs databaseURL inside a command so the root --env-file
// flag is populated the way the CLI populates it.
func resolveDatabaseURL(t *testing.T, envFile string) (string, error) {
	t.Helper()

	var resolved string
	cmd := &cli.Command{
		Flags: []cli.Flag{&cli.StringFlag{Name: "env-file"}},
		Action: func(_ context.Context, cmd *cli.Command) error {
			var err error
			resolved, err = databaseURL(cmd)
			return err
		},
	}
	err := cmd.Run(t.Context(), []string{"tango", "--env-file=" + envFile})
	return resolved, err
}

func TestMigrateUpWithoutDatabaseURL(t *testing.T) {
	out, err := runMigrateUpCmd(t, "")
	require.ErrorIs(t, err, ErrDatabaseURLUnset)
	assert.Empty(t, out)
}

func TestMigrateUpDryRunListsPendingWithoutApplying(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	out, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "00001_initialize_schema.sql")
	assert.Contains(t, out, "9 pending migration(s)")
	assert.NotContains(t, out, "applied")

	// Nothing may have been written.
	db, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	defer func() { require.NoError(t, db.Close()) }()

	migrator, err := database.NewMigrator(t.Context(), db, database.MigratorOptions{})
	require.NoError(t, err)

	version, err := migrator.Version(t.Context())
	require.NoError(t, err)
	assert.Zero(t, version, "--dry-run must not apply anything")
}

func TestMigrateUpAppliesAndIsIdempotent(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "00001_initialize_schema.sql applied")
	assert.Contains(t, out, "9 migration(s) applied")

	out, err = runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "no pending migrations")
}

func TestMigrateUpStopsAtToVersion(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force", "--to=2")
	require.NoError(t, err)
	assert.Contains(t, out, "2 migration(s) applied")
	assert.NotContains(t, out, "00003_")

	out, err = runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "7 migration(s) applied")
}

// A non-interactive run (stdin is not a terminal) must not block on the
// confirmation prompt; task db:migrate and CI depend on it.
func TestMigrateUpDoesNotPromptWithoutTerminal(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateUpCmd(t, "", "--env-file="+envFile)
	require.NoError(t, err)
	assert.NotContains(t, out, "[y/N]")
	assert.Contains(t, out, "9 migration(s) applied")
}

func TestConfirm(t *testing.T) {
	tests := []struct {
		name        string
		stdin       string
		args        []string
		interactive bool
		want        bool
	}{
		{name: "force skips prompt", interactive: true, args: []string{"--force"}, want: true},
		{name: "non-interactive applies", interactive: false, want: true},
		{name: "interactive yes", interactive: true, stdin: "y\n", want: true},
		{name: "interactive yes word", interactive: true, stdin: "yes\n", want: true},
		{name: "interactive no", interactive: true, stdin: "n\n", want: false},
		{name: "interactive empty", interactive: true, stdin: "\n", want: false},
		{name: "interactive eof", interactive: true, stdin: "", want: false},
		{name: "interactive other word", interactive: true, stdin: "maybe\n", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			cmd := &cli.Command{
				Writer: &out,
				Reader: strings.NewReader(tt.stdin),
				Flags:  []cli.Flag{&cli.BoolFlag{Name: "force"}},
			}

			var got bool
			var promptErr error
			cmd.Action = func(_ context.Context, cmd *cli.Command) error {
				got, promptErr = confirm(cmd, tt.interactive, "apply 3 pending migration(s)?")
				return nil
			}
			require.NoError(t, cmd.Run(t.Context(), append([]string{"tango"}, tt.args...)))
			require.NoError(t, promptErr)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMigrateDownRollsBackNewestFirst(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err := runMigrateDownCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "00009_add_session_remember.sql rolled back")
	assert.Contains(t, out, "1 migration(s) rolled back")

	// The queue tables arrive in 00008, which is now the highest applied
	// migration, so they must still exist.
	db, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	var exists bool
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'sessions' AND column_name = 'remember')").Scan(&exists))
	assert.False(t, exists, "00009 must have been rolled back")

	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'queue_tasks')").Scan(&exists))
	assert.True(t, exists, "00008 must still be applied")
}

func TestMigrateDownCountAndDryRun(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err := runMigrateDownCmd(t, "", "--env-file="+envFile, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "00009_add_session_remember.sql")
	assert.Contains(t, out, "1 migration(s) to roll back")

	// --dry-run must not have rolled anything back.
	version := currentVersion(t, dsn)
	assert.Equal(t, int64(9), version)

	out, err = runMigrateDownCmd(t, "", "--env-file="+envFile, "--force", "--count=3")
	require.NoError(t, err)
	assert.Contains(t, out, "00007_create_rate_limits_table.sql rolled back")
	assert.Contains(t, out, "3 migration(s) rolled back")
	assert.Equal(t, int64(6), currentVersion(t, dsn))
}

// A count above what the database has rolls back everything instead of failing.
func TestMigrateDownCountAboveApplied(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err := runMigrateDownCmd(t, "", "--env-file="+envFile, "--force", "--count=50")
	require.NoError(t, err)
	assert.Contains(t, out, "9 migration(s) rolled back")
	assert.Zero(t, currentVersion(t, dsn))
}

func TestMigrateDownRejectsZeroCount(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateDownCmd(t, "", "--env-file="+envFile, "--count=0")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "greater than zero")
	assert.Empty(t, out)
}

func TestMigrateDownWithoutAppliedMigrations(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateDownCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "no applied migrations")
}

// A declined rollback must leave the database untouched. The prompt is
// unreachable from a test without a terminal, so the check is stubbed.
func TestMigrateDownDeclinedLeavesDatabase(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	terminalCheck = func(*cli.Command) bool { return true }
	t.Cleanup(func() { terminalCheck = isTerminal })

	out, err := runMigrateDownCmd(t, "n\n", "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "roll back 1 migration(s)? [y/N]")
	assert.Contains(t, out, "1 migration(s) left applied")
	assert.Equal(t, int64(9), currentVersion(t, dsn))
}

// An accepted rollback applies, proving the prompt gate is not the only path.
func TestMigrateDownAcceptedPrompt(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	terminalCheck = func(*cli.Command) bool { return true }
	t.Cleanup(func() { terminalCheck = isTerminal })

	out, err := runMigrateDownCmd(t, "y\n", "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "00009_add_session_remember.sql rolled back")
	assert.Equal(t, int64(8), currentVersion(t, dsn))
}

func TestMigrateStatus(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	out, err := runMigrateStatusCmd(t, "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "00001 pending 00001_initialize_schema.sql")
	assert.Contains(t, out, "version 00000; 0 of 9 applied")

	_, err = runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err = runMigrateStatusCmd(t, "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "00001 applied 00001_initialize_schema.sql")
	assert.Contains(t, out, "version 00009; 9 of 9 applied")
}

func TestMigrateVersion(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateVersionCmd(t, "--env-file="+envFile)
	require.NoError(t, err)
	assert.Equal(t, "0\n", out)

	_, err = runMigrateUpCmd(t, "", "--env-file="+envFile, "--force", "--to=4")
	require.NoError(t, err)

	out, err = runMigrateVersionCmd(t, "--env-file="+envFile)
	require.NoError(t, err)
	assert.Equal(t, "4\n", out)
}

// The version command prints a bare number so scripts can consume it.
func TestMigrateVersionWithoutDatabaseURL(t *testing.T) {
	out, err := runMigrateVersionCmd(t, "")
	require.ErrorIs(t, err, ErrDatabaseURLUnset)
	assert.Empty(t, out)
}

func runMigrateDownCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateDownCmd, stdin, args...)
}

func runMigrateStatusCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateStatusCmd, "", args...)
}

func runMigrateVersionCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateVersionCmd, "", args...)
}

// currentVersion reads the version through the migrator rather than the raw
// table, so the test cannot pass on a stale row.
func currentVersion(t *testing.T, dsn string) int64 {
	t.Helper()

	db, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	migrator, err := database.NewMigrator(t.Context(), db, database.MigratorOptions{})
	require.NoError(t, err)

	version, err := migrator.Version(t.Context())
	require.NoError(t, err)
	return version
}
