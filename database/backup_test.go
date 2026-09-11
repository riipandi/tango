package database

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requirePGTools skips the backup tests when the PostgreSQL client
// binaries are not installed locally — the tools run on the host,
// against the container's published port.
func requirePGTools(t *testing.T) {
	t.Helper()

	for _, tool := range []string{"pg_dump", "pg_restore", "psql"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not in PATH; install the PostgreSQL client tools", tool)
		}
	}
}

// TestBackupRoundTrip covers the full dump → modify → restore
// cycle plus export and import, against the shared container.
func TestBackupRoundTrip(t *testing.T) {
	requirePGTools(t)
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	// Deterministic starting state: everything applied.
	freshMigratedDB(ctx, t, pg.DSN)

	db, err := sql.Open("pgx", pg.DSN)
	require.NoError(t, err)
	defer db.Close()

	// --- Dump (custom format), all and data. Empty dir selects the
	// default backup directory; the dir-override case is covered
	// below in the same round trip.
	fullDump, err := Dump(ctx, pg.DSN, "all", "")
	require.NoError(t, err)
	info, err := os.Stat(fullDump)
	require.NoError(t, err)
	assert.Greater(t, info.Size(), int64(100), "custom dump must not be empty")

	_, err = Dump(ctx, pg.DSN, "data", t.TempDir())
	require.NoError(t, err)

	_, err = Dump(ctx, pg.DSN, "bogus", "")
	assert.ErrorContains(t, err, "unknown dump mode")

	// --- Export (plain SQL), all and data.
	fullSQL, err := Export(ctx, pg.DSN, "all", "")
	require.NoError(t, err)
	exported, err := os.ReadFile(fullSQL)
	require.NoError(t, err)
	assert.Contains(t, string(exported), "app_migration", "export must contain the metadata table")

	_, err = Export(ctx, pg.DSN, "data", t.TempDir())
	require.NoError(t, err)

	// --- Restore (custom format): objects captured in the dump
	// return to their dumped shape (--clean drops and recreates
	// them). A marker table exists at dump time, then diverges;
	// restore must revert the divergence.
	_, err = db.Exec("CREATE TABLE backup_marker_test (id int)")
	require.NoError(t, err)

	fullDump, err = Dump(ctx, pg.DSN, "all", "")
	require.NoError(t, err)
	_, err = db.Exec("ALTER TABLE backup_marker_test ADD COLUMN extra int")
	require.NoError(t, err)

	require.NoError(t, Restore(ctx, pg.DSN, "all", fullDump))

	var columns int
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM information_schema.columns WHERE table_name = 'backup_marker_test' AND column_name = 'extra'",
	).Scan(&columns))
	assert.Equal(t, 0, columns, "restore must revert post-dump schema changes")

	_, err = db.Exec("DROP TABLE backup_marker_test")
	require.NoError(t, err)

	// --- Import: a plain SQL file is executed as-is, and a
	// failing statement aborts the import (ON_ERROR_STOP=on).
	sqlFile := filepath.Join(t.TempDir(), "seed.sql")
	require.NoError(t, os.WriteFile(sqlFile, []byte(
		"CREATE TABLE import_marker_test (id int);\nINSERT INTO import_marker_test VALUES (42);\n",
	), 0o644))
	require.NoError(t, Import(ctx, pg.DSN, sqlFile))

	var value int
	require.NoError(t, db.QueryRow("SELECT id FROM import_marker_test LIMIT 1").Scan(&value))
	assert.Equal(t, 42, value)

	badFile := filepath.Join(t.TempDir(), "bad.sql")
	require.NoError(t, os.WriteFile(badFile, []byte("SELECT * FROM missing_table_xyz;\n"), 0o644))
	assert.Error(t, Import(ctx, pg.DSN, badFile), "failing statement must abort the import")
}

// TestRestoreDryRunCommands verifies the command renderers used by
// --dry-run: they validate inputs without touching anything.
func TestRestoreDryRunCommands(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	dump := filepath.Join(t.TempDir(), "x.dump")
	require.NoError(t, os.WriteFile(dump, []byte("x"), 0o644))

	cmd, err := RestoreCommand(pg.DSN, "all", dump)
	require.NoError(t, err)
	assert.Contains(t, cmd, "pg_restore")
	assert.Contains(t, cmd, "--clean")
	assert.NotContains(t, cmd, "securedb", "the password must never appear in argv")

	_, err = RestoreCommand(pg.DSN, "bogus", dump)
	assert.ErrorContains(t, err, "unknown restore mode")
	_, err = RestoreCommand(pg.DSN, "all", filepath.Join(t.TempDir(), "missing.dump"))
	assert.ErrorContains(t, err, "dump file")

	sqlFile := filepath.Join(t.TempDir(), "x.sql")
	require.NoError(t, os.WriteFile(sqlFile, []byte("SELECT 1;"), 0o644))
	cmd, err = ImportCommand(pg.DSN, sqlFile)
	require.NoError(t, err)
	assert.Contains(t, cmd, "psql")
	assert.Contains(t, cmd, "ON_ERROR_STOP=on")
}
