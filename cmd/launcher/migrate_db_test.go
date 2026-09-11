package launcher

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateDBCommandGrammar locks in the migrate db command
// group: the four subcommands parse with their modes and file
// arguments. It runs in both build variants — backup tooling is
// available everywhere.
func TestMigrateDBCommandGrammar(t *testing.T) {
	require.Equal(t, "migrate db dump <mode>", parseOnly(t, "migrate", "db", "dump", "all"))
	require.Equal(t, "migrate db dump <mode>", parseOnly(t, "migrate", "db", "dump", "data"))
	require.Equal(t, "migrate db restore <mode> <file>", parseOnly(t, "migrate", "db", "restore", "all", "storage/backup/x.dump"))
	require.Equal(t, "migrate db restore <mode> <file>", parseOnly(t, "migrate", "db", "restore", "data", "--force", "x.dump"))
	require.Equal(t, "migrate db restore <mode> <file>", parseOnly(t, "migrate", "db", "restore", "schema", "--dry-run", "x.dump"))
	require.Equal(t, "migrate db export <mode>", parseOnly(t, "migrate", "db", "export", "all"))
	require.Equal(t, "migrate db import <file>", parseOnly(t, "migrate", "db", "import", "storage/backup/x.sql"))
}

// requirePGTools skips the test when the PostgreSQL client
// binaries are not installed locally.
func requirePGTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"pg_dump", "pg_restore", "psql"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not in PATH; install the PostgreSQL client tools", tool)
		}
	}
}

// TestMigrateDBLifecycle exercises the backup commands end to end
// against the shared testcontainer database: dump, a failing mode,
// and the gated restore/import paths. It runs in both build
// variants — the db group is no longer debug-only.
func TestMigrateDBLifecycle(t *testing.T) {
	requirePGTools(t)
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)

	// Migrate first: the backup, restore, and export round-trips
	// below need the schema (and the metadata table) to exist.
	_, err := runMigrate(t, false, nil, "migrate", "up")
	require.NoError(t, err)

	// Dump: a real file lands in storage/backup.
	out, err := runMigrate(t, false, nil, "migrate", "db", "dump", "all")
	require.NoError(t, err)
	assert.Contains(t, out, "dumped")
	dumpFile := backupPathFromOutput(t, out)
	info, statErr := os.Stat(dumpFile)
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(100), "dump must not be empty")
	t.Cleanup(func() { _ = os.Remove(dumpFile) })

	// Unknown mode is a usage error, before any binary runs.
	_, err = runMigrate(t, false, nil, "migrate", "db", "dump", "bogus")
	assert.ErrorContains(t, err, "unknown dump mode")

	// Restore and import are destructive: non-interactive stdin
	// refuses without --force.
	_, err = runMigrate(t, false, nil, "migrate", "db", "restore", "all", dumpFile)
	assert.ErrorContains(t, err, "--force")

	_, err = runMigrate(t, false, nil, "migrate", "db", "import", dumpFile)
	assert.ErrorContains(t, err, "--force")

	// Dry-run renders the exact command without running it.
	out, err = runMigrate(t, false, nil, "migrate", "db", "restore", "all", "--dry-run", dumpFile)
	require.NoError(t, err)
	assert.Contains(t, out, "pg_restore")
	assert.Contains(t, out, "--clean")

	// Restore with --force round-trips the dump back.
	out, err = runMigrate(t, false, nil, "migrate", "db", "restore", "all", "--force", dumpFile)
	require.NoError(t, err)
	assert.Contains(t, out, "restore completed")

	// Export produces a plain SQL file mentioning the metadata table.
	out, err = runMigrate(t, false, nil, "migrate", "db", "export", "all")
	require.NoError(t, err)
	assert.Contains(t, out, "exported")
	sqlFile := backupPathFromOutput(t, out)
	exported, readErr := os.ReadFile(sqlFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(exported), "app_migration")
	t.Cleanup(func() { _ = os.Remove(sqlFile) })
}

// backupPathFromOutput extracts the storage/backup path from a
// dump/export command's stdout.
func backupPathFromOutput(t *testing.T, out string) string {
	t.Helper()

	idx := strings.Index(out, "storage/")
	require.NotEqual(t, -1, idx, "output must contain the backup path: %q", out)
	return strings.TrimSpace(out[idx:])
}
