//go:build debug

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/envfile"
	"github.com/riipandi/tango/pkg/testutils"
)

// migrate:validate must not need a database, so it works before one exists.
func TestMigrateValidateNeedsNoDatabase(t *testing.T) {
	t.Setenv(envfile.DatabaseURL, "")
	t.Setenv("HOME", t.TempDir())

	out, err := runMigrateValidateCmd(t)
	require.NoError(t, err)
	assert.Contains(t, out, "9 migration file(s) valid")
}

func TestMigrateValidateReportsNoIssues(t *testing.T) {
	out, err := runMigrateValidateCmd(t)
	require.NoError(t, err)
	assert.NotContains(t, out, "goose will skip")
	assert.Contains(t, out, "9 migration file(s) valid")
}

// A broken file must fail the command, so `task check` fails with it.
func TestMigrateValidateReportsIssuesAndFails(t *testing.T) {
	migrationCheck = func() database.ValidationReport {
		return database.ValidationReport{
			Checked: 1,
			Issues: []database.ValidationIssue{
				{File: "00001_x.sql", Line: 3, Message: "boom"},
			},
		}
	}
	t.Cleanup(func() { migrationCheck = database.Validate })

	out, err := runMigrateValidateCmd(t)
	require.Error(t, err)
	assert.Contains(t, out, "00001_x.sql:3: boom")
	assert.Contains(t, err.Error(), "1 problem(s) in 1 migration file(s)")
}

func TestMigrateResetRollsBackEverything(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err := runMigrateResetCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "9 migration(s) rolled back")
	assert.Zero(t, currentVersion(t, dsn))
}

// --up rebuilds the schema in one command.
func TestMigrateResetWithUpReappliesEverything(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	out, err := runMigrateResetCmd(t, "", "--env-file="+envFile, "--force", "--up")
	require.NoError(t, err)
	assert.Contains(t, out, "9 migration(s) rolled back")
	assert.Contains(t, out, "9 migration(s) applied")
	assert.Equal(t, int64(9), currentVersion(t, dsn))
}

func TestMigrateResetDryRunChangesNothing(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force", "--to=3")
	require.NoError(t, err)

	out, err := runMigrateResetCmd(t, "", "--env-file="+envFile, "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "00003_create_multifactor_tables.sql")
	assert.Contains(t, out, "3 migration(s) to roll back")
	assert.Equal(t, int64(3), currentVersion(t, dsn))

	// With --up the pending half is listed too, still without touching anything.
	out, err = runMigrateResetCmd(t, "", "--env-file="+envFile, "--dry-run", "--up")
	require.NoError(t, err)
	assert.Contains(t, out, "6 pending migration(s)")
	assert.Equal(t, int64(3), currentVersion(t, dsn))
}

func TestMigrateResetWithoutAppliedMigrations(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := writeEnvFile(t, container.NewDatabase(t))

	out, err := runMigrateResetCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "no applied migrations")
}

func TestMigrateResetDeclinedLeavesDatabase(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)
	envFile := writeEnvFile(t, dsn)

	_, err := runMigrateUpCmd(t, "", "--env-file="+envFile, "--force")
	require.NoError(t, err)

	terminalCheck = func(*cli.Command) bool { return true }
	t.Cleanup(func() { terminalCheck = isTerminal })

	out, err := runMigrateResetCmd(t, "n\n", "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "roll back all 9 migration(s)? [y/N]")
	assert.Contains(t, out, "9 migration(s) left applied")
	assert.Equal(t, int64(9), currentVersion(t, dsn))
}

func runMigrateResetCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateResetCmd, stdin, args...)
}

func runMigrateValidateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runMigrateCmd(t, migrateValidateCmd, "", args...)
}
