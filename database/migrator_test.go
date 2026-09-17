//go:build !debug

// Package database provides release migration operations against
// compiled-in SQL files.
package database

// TestMigrationsLifecycle applies the embedded migrations (release
// surface), inspects the resulting schemas, rolls back, and
// re-applies. Runs against the shared testcontainers Postgres,
// never against the compose stack.

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
	require.Len(t, applied, 8)
	assert.Equal(t, int64(1), applied[0].Version)
	assert.Contains(t, applied[0].Path, "initialize_schema")
	assert.Equal(t, int64(2), applied[1].Version)
	assert.Contains(t, applied[1].Path, "identity_tables")
	assert.Equal(t, int64(3), applied[2].Version)
	assert.Contains(t, applied[2].Path, "webauthn_mfa_tables")
	assert.Equal(t, int64(4), applied[3].Version)
	assert.Contains(t, applied[3].Path, "federation_tables")
	assert.Equal(t, int64(5), applied[4].Version)
	assert.Contains(t, applied[4].Path, "admin_tables")
	assert.Equal(t, int64(6), applied[5].Version)
	assert.Contains(t, applied[5].Path, "webhook_tables")
	assert.Equal(t, int64(7), applied[6].Version)
	assert.Contains(t, applied[6].Path, "rate_limits_table")
	assert.Equal(t, int64(8), applied[7].Version)
	assert.Contains(t, applied[7].Path, "queue_tables")

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

	// All tables land in the public schema, asserted by name so test litter cannot skew the count.
	for schema, names := range map[string][]string{
		"public": {
			"deleted_records", "app_config",
			"users", "user_passwords", "user_groups", "user_groups_users",
			"sessions", "auth_tokens", "signup_tokens",
			"signup_tokens_user_groups", "audit_logs",
			"webhook_endpoints", "webhook_deliveries", "webhook_delivery_attempts", "oidc_device_codes", "jwks",
			"webauthn_credentials",
			"webauthn_sessions", "oidc_clients",
			"custom_claims", "oidc_authorization_codes",
			"user_authorized_oidc_clients", "oidc_clients_allowed_user_groups",
			"oidc_refresh_tokens", "scim_service_providers",
			"api_keys", "rate_limits", "queue_tasks", "queue_tasks_completed",
			"oauth2_sessions", "oauth2_jtis", "interaction_sessions",
			"device_login_requests", "apis", "api_permissions",
			"user_groups_allowed_oidc_clients",
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

	// The down target is read-only: after it reports, the migration is still applied.
	target, err := MigrateDownTarget(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, int64(8), target.Version)
	assert.Equal(t, "applied", target.State)

	// Roll back the most recent migration, verify the state flipped to pending, then re-apply.
	outcome, err := MigrateDown(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, outcome)
	assert.Equal(t, int64(8), outcome.Version)

	target, err = MigrateDownTarget(ctx, pg.DSN)
	require.NoError(t, err)
	require.NotNil(t, target)
	assert.Equal(t, int64(7), target.Version, "next down target follows the rollback")

	statuses, err := MigrateStatus(ctx, pg.DSN)
	require.NoError(t, err)
	require.Len(t, statuses, 8)
	assert.Equal(t, "pending", statuses[7].State)

	reapplied, err := MigrateUp(ctx, pg.DSN)
	require.NoError(t, err)
	assert.Len(t, reapplied, 1, "re-applied after rollback")
}
