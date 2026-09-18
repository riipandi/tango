//go:build debug

package database

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateMigrationSequential verifies the incremental naming:
// the first scaffold is 00001, the next one continues from the
// highest existing version, and the created path is returned.
func TestCreateMigrationSequential(t *testing.T) {
	dir := t.TempDir()

	path, err := createMigration("", "add_users_table", dir)
	require.NoError(t, err)
	assert.Equal(t, "00001_add_users_table.sql", filepath.Base(path))

	path, err = createMigration("", "add_indexes", dir)
	require.NoError(t, err)
	assert.Equal(t, "00002_add_indexes.sql", filepath.Base(path))
}

// TestCreateMigrationContinuesFromHighest seeds an existing
// migration and expects the next version to follow it.
func TestCreateMigrationContinuesFromHighest(t *testing.T) {
	dir := t.TempDir()
	existing := filepath.Join(dir, "00041_add_schemas.sql")
	require.NoError(t, os.WriteFile(existing, []byte("-- existing\n"), 0o644))

	path, err := createMigration("", "next_one", dir)
	require.NoError(t, err)
	assert.Equal(t, "00042_next_one.sql", filepath.Base(path))
}

// TestCreateMigrationRequiresName rejects empty names.
func TestCreateMigrationRequiresName(t *testing.T) {
	_, err := createMigration("", "", t.TempDir())
	assert.ErrorContains(t, err, "name is required")
}

// TestMigrateValidateAndFix covers the file-quality checks and the
// reorder command against the real migrations directory.
func TestMigrateValidateAndFix(t *testing.T) {
	chdirRepoRoot(t)

	require.NoError(t, Validate(), "repository migrations must validate")
	require.NoError(t, Fix(), "sequential migrations need no reordering")
}

// TestValidateDirRejectsProblems covers the validate rules with a
// synthetic directory.
func TestValidateDirRejectsProblems(t *testing.T) {
	dir := t.TempDir()

	good := "-- +goose Up\n-- +goose Down\nSELECT 1;\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00001_good.sql"), []byte(good), 0o644))
	require.NoError(t, validateDir(dir))

	// Duplicate version.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00001_dupe.sql"), []byte(good), 0o644))
	assert.ErrorContains(t, validateDir(dir), "duplicate version")
	require.NoError(t, os.Remove(filepath.Join(dir, "00001_dupe.sql")))

	// Missing Down annotation.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "00002_nodown.sql"), []byte("-- +goose Up\nSELECT 1;\n"), 0o644))
	assert.ErrorContains(t, validateDir(dir), "+goose Down")

	// Non-sequential naming.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "20260101120000_ts.sql"), []byte(good), 0o644))
	assert.ErrorContains(t, validateDir(dir), "sequential naming")
}

// TestMigrateReset rolls the schema back to the initial state and
// re-applies from scratch (disk source, resolved from repo root).
func TestMigrateReset(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	applied := freshMigratedDB(ctx, t, pg.DSN)

	rolled, err := MigrateReset(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, rolled, len(applied))

	reapplied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, reapplied, len(applied))
}

// TestMigrateDownTo verifies the up-to/down-to pair: applying only
// up to a version and rolling back above one.
func TestMigrateDownTo(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	applied := freshMigratedDB(ctx, t, pg.DSN)
	highest := applied[len(applied)-1].Version

	rolled, err := MigrateDownTo(ctx, pg.DSN, highest-1)
	require.NoError(t, err)
	assert.Len(t, rolled, 1)

	reapplied, err := MigrateUpTo(ctx, pg.DSN, highest)
	require.NoError(t, err)
	assert.Len(t, reapplied, 1)
}

// TestMigrateVersion reports the current and target versions.
func TestMigrateVersion(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	freshMigratedDB(ctx, t, pg.DSN)

	current, target, err := MigrateVersion(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Greater(t, current, int64(0))
	assert.GreaterOrEqual(t, target, current)
}
