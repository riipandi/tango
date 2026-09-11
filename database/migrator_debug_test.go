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

// chdirRepoRoot moves the test working directory to the repository
// root, where the disk-based migration source resolves, and
// restores it when the test ends.
func chdirRepoRoot(t *testing.T) {
	t.Helper()

	orig, err := os.Getwd()
	require.NoError(t, err)

	dir := orig
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "Taskfile.yml")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

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

// TestMigrateReset rolls the schema back to the initial state and
// re-applies from scratch (disk source, resolved from repo root).
func TestMigrateReset(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	applied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotEmpty(t, applied)

	rolled, err := MigrateReset(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, rolled, len(applied))

	reapplied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, reapplied, len(applied))
}
