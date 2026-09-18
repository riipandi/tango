package launcher

import (
	"os"
	"strings"
	"testing"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDBLifecycle exercises the manage commands end to end
// against the shared testcontainer database: dump, a failing mode,
// and the gated restore/import paths. Runs in both build variants.
func TestDBLifecycle(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)
	// The CLI runs client tools through the container, so no host
	// PostgreSQL installation is required.
	prevExecutor := database.DefaultExecutor
	database.DefaultExecutor = &testutils.ContainerExecutor{PG: pg}
	t.Cleanup(func() { database.DefaultExecutor = prevExecutor })

	// Isolate on-disk state: backups land in a temp data root.
	dataDir := t.TempDir()
	t.Setenv("APP_DATA_DIR", dataDir)

	// Migrate first: the backup, restore, and export round-trips
	// below need the schema (and the metadata table) to exist.
	_, err := runMigrate(t, false, nil, "db", "migrate:up")
	require.NoError(t, err)

	// Dump: a real file lands in <data-dir>/backup.
	out, err := runMigrate(t, false, nil, "db", "dump", "all")
	require.NoError(t, err)
	assert.Contains(t, out, "dumped")
	dumpFile := backupPathFromOutput(t, out)
	assert.Contains(t, dumpFile, dataDir, "dump must land under the configured data root")
	info, statErr := os.Stat(dumpFile)
	require.NoError(t, statErr)
	assert.Greater(t, info.Size(), int64(100), "dump must not be empty")
	t.Cleanup(func() { _ = os.Remove(dumpFile) })

	// Unknown mode is a usage error, before any binary runs.
	_, err = runMigrate(t, false, nil, "db", "dump", "bogus")
	assert.ErrorContains(t, err, "unknown dump mode")

	// Restore and import are destructive: non-interactive stdin
	// refuses without --force.
	_, err = runMigrate(t, false, nil, "db", "restore", "all", dumpFile)
	assert.ErrorContains(t, err, "--force")

	_, err = runMigrate(t, false, nil, "db", "import", dumpFile)
	assert.ErrorContains(t, err, "--force")

	// Dry-run renders the exact command without running it.
	out, err = runMigrate(t, false, nil, "db", "restore", "all", "--dry-run", dumpFile)
	require.NoError(t, err)
	assert.Contains(t, out, "pg_restore")
	assert.Contains(t, out, "--clean")

	// Restore with --force round-trips the dump back.
	out, err = runMigrate(t, false, nil, "db", "restore", "all", "--force", dumpFile)
	require.NoError(t, err)
	assert.Contains(t, out, "restore completed")

	// Export produces a plain SQL file mentioning the metadata table.
	out, err = runMigrate(t, false, nil, "db", "export", "all")
	require.NoError(t, err)
	assert.Contains(t, out, "exported")
	sqlFile := backupPathFromOutput(t, out)
	exported, readErr := os.ReadFile(sqlFile)
	require.NoError(t, readErr)
	assert.Contains(t, string(exported), "app_migration")
	t.Cleanup(func() { _ = os.Remove(sqlFile) })
}

// backupPathFromOutput extracts the backup path from
// dump/export stdout: the last field after the marker.
func backupPathFromOutput(t *testing.T, out string) string {
	t.Helper()

	line := out
	if idx := strings.LastIndex(out, "dumped"); idx >= 0 {
		line = out[idx:]
	} else if idx := strings.LastIndex(out, "exported"); idx >= 0 {
		line = out[idx:]
	}
	fields := strings.Fields(line)
	require.NotEmpty(t, fields, "output must contain the backup path: %q", out)
	return strings.TrimSpace(fields[len(fields)-1])
}
