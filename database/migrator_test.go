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

	// Deterministic starting state: the backup tests share this
	// container, so roll back anything they applied first.
	if _, err := MigrateDownTo(ctx, pg.DSN, 0); err != nil {
		t.Fatalf("reset before test: %v", err)
	}

	applied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	require.Len(t, applied, 21)
	assert.Equal(t, int64(1), applied[0].Version)
	assert.Contains(t, applied[0].Path, "initialize_schema")
	assert.Equal(t, int64(21), applied[20].Version)
	assert.Contains(t, applied[20].Path, "queue_tables")

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

	// All tables land in the public schema (reference layout:
	// domainaja-app), asserted by name so test litter cannot skew
	// the count.
	for schema, names := range map[string][]string{
		"public": {
			"deleted_records", "app_settings",
			"users", "user_passwords", "user_groups", "user_groups_users",
			"user_phones", "sessions", "auth_tokens", "signup_tokens",
			"signup_tokens_user_groups", "refresh_tokens", "audit_logs",
			"file_stores", "webhook_events", "webhook_logs", "jwks",
			"invitations", "mfa_keys", "webauthn_credentials",
			"webauthn_sessions", "oauth_connections", "oidc_clients",
			"custom_claims", "oidc_authorization_codes",
			"user_authorized_oidc_clients", "oidc_clients_allowed_user_groups",
			"oidc_refresh_tokens", "oidc_device_codes", "scim_service_providers",
			"api_keys", "rate_limits", "queue_tasks", "queue_tasks_completed",
		},
	} {
		var found int
		require.NoError(t, db.QueryRow(
			"SELECT count(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name = ANY($2)",
			schema, names,
		).Scan(&found), "count tables in %s", schema)
		assert.Equal(t, len(names), found, "expected tables in the %s schema", schema)
	}

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
	assert.Equal(t, int64(21), target.Version)
	assert.Equal(t, "applied", target.State)

	// Roll back the most recent migration, verify the state
	// flipped to pending, then re-apply.
	outcome, err := MigrateDown(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, int64(21), outcome.Version)

	target, err = MigrateDownTarget(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, int64(20), target.Version, "next down target follows the rollback")

	statuses, err := MigrateStatus(ctx, pg.DSN)
	require.NoError(t, err)
	require.Len(t, statuses, 21)
	assert.Equal(t, "pending", statuses[20].State)

	reapplied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, reapplied, 1, "re-applied after rollback")
}
