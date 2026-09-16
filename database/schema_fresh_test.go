package database

import (
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFreshSchemaHasNoObsoleteTables applies all migrations to a
// fresh database and asserts the obsolete shapes are gone.
func TestFreshSchemaHasNoObsoleteTables(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	if _, err := MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	conn, err := pgx.Connect(ctx, pg.DSN)
	require.NoError(t, err)
	defer conn.Close(ctx)

	for _, table := range []string{
		"app_settings", "user_phones", "invitations", "mfa_keys",
		"oauth_connections", "oidc_device_codes", "file_stores", "refresh_tokens",
	} {
		var count int
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public' AND table_name = $1",
			table).Scan(&count))
		assert.Zero(t, count, "obsolete table %s must not exist", table)
	}

	// The dead session columns are gone.
	var dead int
	require.NoError(t, conn.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sessions'
		  AND column_name IN ('totp_pending', 'oauth_groups', 'oauth_name', 'oauth_sub')`).Scan(&dead))
	assert.Zero(t, dead, "dead session columns must not exist")

	// Session ids are UUIDs now.
	var idType string
	require.NoError(t, conn.QueryRow(ctx, `
		SELECT data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'sessions' AND column_name = 'id'`).Scan(&idType))
	assert.Equal(t, "uuid", idType)
}
