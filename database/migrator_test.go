//go:build !debug

// TestMigrationsLifecycle applies the embedded migrations (release
// surface), inspects the resulting schemas, rolls back, and
// re-applies. The test runs against the shared testcontainers
// Postgres, never against the compose stack.
package database

import (
	"database/sql"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigrationsLifecycle(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	applied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	require.Len(t, applied, 1)
	assert.Equal(t, int64(1), applied[0].Version)
	assert.Contains(t, applied[0].Path, "initialize_schema")

	db, err := sql.Open("pgx", pg.DSN)
	require.NoError(t, err)
	defer db.Close()

	// The initial migration registers extensions and creates the
	// application schemas the search path expects.
	for _, schema := range []string{"auth", "reference", "scheduler"} {
		var count int
		require.NoError(t, db.QueryRow(
			"SELECT count(*) FROM pg_namespace WHERE nspname = $1", schema,
		).Scan(&count), "check schema %s", schema)
		assert.Equal(t, 1, count, "schema %s must exist after migrate up", schema)
	}

	var extensions int
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM pg_extension WHERE extname IN ('citext', 'hstore', 'pg_trgm', 'pgcrypto')",
	).Scan(&extensions))
	assert.Equal(t, 4, extensions)

	// The metadata table is the project-renamed one, not goose's
	// goose_db_version default.
	var tables int
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'app_migration'",
	).Scan(&tables))
	assert.Equal(t, 1, tables, "metadata table must be app_migration")
	require.NoError(t, db.QueryRow(
		"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'goose_db_version'",
	).Scan(&tables))
	assert.Equal(t, 0, tables, "goose_db_version must not exist")

	// The down target is read-only: after it reports, the
	// migration is still applied.
	target, err := MigrateDownTarget(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, int64(1), target.Version)
	assert.Equal(t, "applied", target.State)

	// Roll back the most recent migration, verify the state
	// flipped to pending, then re-apply.
	outcome, err := MigrateDown(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, int64(1), outcome.Version)

	target, err = MigrateDownTarget(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Nil(t, target, "nothing applied after rollback")

	statuses, err := MigrateStatus(ctx, pg.DSN)
	require.NoError(t, err)
	require.Len(t, statuses, 1)
	assert.Equal(t, "pending", statuses[0].State)

	reapplied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, reapplied, 1, "re-applied after rollback")
}
