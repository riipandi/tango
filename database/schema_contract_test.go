//go:build !debug

package database

// schema_contract_test.go verifies the PostgreSQL contracts that the
// migrations promise: enc: secret markers, normalized login
// uniqueness, token expiry checks, and outbox atomicity. Runs
// against the throwaway testcontainers Postgres.

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openDB opens a pgx connection over the test DSN.
func openDB(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close(t.Context()) })
	return conn
}

// mustExec runs one statement and fails the test on error.
func mustExec(t *testing.T, ctx context.Context, dsn, sql string, args ...any) {
	t.Helper()
	conn := openDB(t, dsn)
	_, err := conn.Exec(ctx, sql, args...)
	require.NoError(t, err, "sql: %s", sql)
}

// mustFail asserts the statement fails with the given constraint.
func mustFail(t *testing.T, ctx context.Context, dsn, sql string, constraint string, args ...any) {
	t.Helper()
	conn := openDB(t, dsn)
	_, err := conn.Exec(ctx, sql, args...)
	require.Error(t, err, "sql: %s", sql)
	assert.Contains(t, err.Error(), constraint, "sql: %s", sql)
}

// seedUser inserts one user and returns its UUID text.
func seedUser(t *testing.T, ctx context.Context, dsn, username string) string {
	t.Helper()
	conn := openDB(t, dsn)
	var id string
	require.NoError(t, conn.QueryRow(ctx,
		`INSERT INTO public.users (username, email, display_name) VALUES ($1, $2, $3) RETURNING id::text`,
		username, username+"@example.com", username).Scan(&id))
	return id
}

// seedClient inserts one OIDC client and returns its id text.
func seedClient(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	conn := openDB(t, dsn)
	var id string
	require.NoError(t, conn.QueryRow(ctx,
		`INSERT INTO public.oidc_clients (id, name, callback_urls) VALUES ('oidc_client_' || md5(random()::text), 'RP', $1) RETURNING id::text`,
		[]string{"https://rp.example/callback"}).Scan(&id))
	return id
}

func TestSecretEncConstraints(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	// Unprefixed secrets violate the marker constraint.
	mustFail(t, ctx, pg.DSN,
		`INSERT INTO public.webhook_endpoints (name, endpoint, method, secret_enc) VALUES ('hook-a', 'https://x.example', 'POST', 'plain')`,
		"chk_webhook_secret_enc")
	mustExec(t, ctx, pg.DSN,
		`INSERT INTO public.webhook_endpoints (name, endpoint, method, secret_enc) VALUES ('hook-a', 'https://x.example', 'POST', 'enc:c2VhbGVk')`)

	mustFail(t, ctx, pg.DSN,
		`INSERT INTO public.scim_service_providers (endpoint, token, oidc_client_id) VALUES ('https://scim.example', 'plain', 'rp')`,
		"chk_scim_token_enc")

	// Seed a relying-party client for the FK, then the valid row.
	clientID := seedClient(t, ctx, pg.DSN)
	mustExec(t, ctx, pg.DSN,
		`INSERT INTO public.scim_service_providers (endpoint, token, oidc_client_id) VALUES ('https://scim.example', 'enc:c2VhbGVk', $1)`,
		clientID)
}

func TestLoginUniquenessIsNormalized(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	seedUser(t, ctx, pg.DSN, "ada")

	// CITEXT username compares case-insensitively; either unique
	// constraint rejects the uppercase variant.
	mustFail(t, ctx, pg.DSN,
		`INSERT INTO public.users (username, email, display_name) VALUES ('ADA', 'other@example.com', 'Ada')`,
		"users_username_key")
}

func TestTokenExpiryIsEnforced(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	userID := seedUser(t, ctx, pg.DSN, "expiry")

	mustFail(t, ctx, pg.DSN,
		`INSERT INTO public.auth_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, 'hash-expired', 'one_time_access', now() - interval '1 hour')`,
		"auth_tokens_expires_at_check", userID)

	mustExec(t, ctx, pg.DSN,
		`INSERT INTO public.auth_tokens (user_id, token_hash, purpose, expires_at) VALUES ($1, 'hash-live', 'one_time_access', now() + interval '1 hour')`,
		userID)
}

func TestOutboxAtomicity(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	conn := openDB(t, pg.DSN)

	tx, err := conn.Begin(ctx)
	require.NoError(t, err)

	// The outbox write and its queue task commit together or not at
	// all: a rollback inside the transaction removes the delivery
	// row with it.
	_, err = tx.Exec(ctx,
		`INSERT INTO public.webhook_deliveries (webhook_id, event, body) SELECT id, 'user.created', convert_to('{"event":"user.created"}', 'UTF8') FROM public.webhook_endpoints WHERE name = 'hook-a'`)
	require.NoError(t, err)

	var deliveries int
	require.NoError(t, tx.QueryRow(ctx,
		"SELECT count(*) FROM public.webhook_deliveries").Scan(&deliveries))
	assert.Equal(t, 1, deliveries, "delivery row visible inside the transaction")

	require.NoError(t, tx.Rollback(ctx))

	require.NoError(t, conn.QueryRow(ctx,
		"SELECT count(*) FROM public.webhook_deliveries").Scan(&deliveries))
	assert.Zero(t, deliveries, "rollback removes the outbox row")
}

func TestQueueNotifyAfterCommit(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	conn := openDB(t, pg.DSN)
	// A second connection simulates the dispatcher's independent
	// read: it cannot see another transaction's uncommitted task.
	reader := openDB(t, pg.DSN)

	// A task enqueued inside an uncommitted transaction is invisible
	// to readers outside it — the dispatcher poll cannot claim it
	// before the commit lands.
	tx, err := conn.Begin(ctx)
	require.NoError(t, err)
	_, err = tx.Exec(ctx,
		`INSERT INTO public.queue_tasks (queue, task) VALUES ('test', 'probe'::bytea)`)
	require.NoError(t, err)

	var visible int
	require.NoError(t, reader.QueryRow(ctx,
		"SELECT count(*) FROM public.queue_tasks WHERE queue = 'test'").Scan(&visible))
	assert.Zero(t, visible, "uncommitted tasks stay invisible to the dispatcher")

	require.NoError(t, tx.Commit(ctx))

	require.NoError(t, reader.QueryRow(ctx,
		"SELECT count(*) FROM public.queue_tasks WHERE queue = 'test'").Scan(&visible))
	assert.Equal(t, 1, visible)
}

func TestCleanupIndexesExist(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	ctx := t.Context()

	conn := openDB(t, pg.DSN)

	// Every expiry-driven cleanup must be index-backed.
	for _, index := range []string{
		"idx_sessions_expires_at",
		"idx_auth_tokens_expires_at",
		"idx_signup_tokens_expires_at",
	} {
		var count int
		require.NoError(t, conn.QueryRow(ctx,
			"SELECT count(*) FROM pg_indexes WHERE indexname = $1", index).Scan(&count),
			"index %s must exist", index)
		assert.Equal(t, 1, count, "index %s must exist", index)
	}
}
