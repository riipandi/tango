package datastore

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startStore connects to the shared testcontainers Postgres and
// fails the test when the container (docker daemon) is unavailable.
func startStore(t *testing.T) *Postgres {
	t.Helper()

	pg := testutils.StartPostgres(t.Context(), t)
	store, err := New(t.Context(), Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// testTable derives a unique, SQL-safe table name per test so the
// shared database needs no cross-test coordination.
func testTable(t *testing.T) string {
	t.Helper()

	replacer := strings.NewReplacer("/", "_", "=", "_", " ", "_", "#", "_", ".", "_")
	return fmt.Sprintf("datastore_%s", replacer.Replace(strings.ToLower(t.Name())))
}

// TestNewFailsFastOnUnreachableHost proves the construction ping:
// a connection to a closed loopback port must fail in New, not on
// the first query.
func TestNewFailsFastOnUnreachableHost(t *testing.T) {
	store, err := New(t.Context(), Options{
		DSN: "postgresql://postgres:postgres@127.0.0.1:59999/postgres?sslmode=disable&connect_timeout=1",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "ping 127.0.0.1")
	assert.Nil(t, store)
}

// TestPostgresExecutorRoundTrip exercises all three Executor
// operations against a real server.
func TestPostgresExecutorRoundTrip(t *testing.T) {
	store := startStore(t)
	ctx := t.Context()
	table := testTable(t)

	_, err := store.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id int primary key, name text)", table))
	require.NoError(t, err)

	var id int
	err = store.QueryRow(ctx,
		fmt.Sprintf("INSERT INTO %s VALUES ($1, $2) RETURNING id", table),
		1, "dummy").Scan(&id)
	require.NoError(t, err)
	assert.Equal(t, 1, id)

	rows, err := store.Query(ctx, fmt.Sprintf("SELECT name FROM %s WHERE id = $1", table), 1)
	require.NoError(t, err)
	defer rows.Close()

	require.True(t, rows.Next())
	var name string
	require.NoError(t, rows.Scan(&name))
	assert.Equal(t, "dummy", name)
	require.False(t, rows.Next())
	require.NoError(t, rows.Err())
}

// TestWithTxCommits verifies the happy path: data written through
// the transactional Executor is visible after WithTx returns nil.
func TestWithTxCommits(t *testing.T) {
	store := startStore(t)
	ctx := t.Context()
	table := testTable(t)

	_, err := store.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id int primary key)", table))
	require.NoError(t, err)

	require.NoError(t, store.WithTx(ctx, func(tx Executor) error {
		_, err := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s VALUES (1)", table))
		return err
	}))

	var count int
	require.NoError(t, store.QueryRow(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count))
	assert.Equal(t, 1, count)
}

// TestWithTxRollsBackOnError verifies the atomicity contract: an
// error inside fn rolls the whole transaction back and surfaces
// the caller's error unchanged.
func TestWithTxRollsBackOnError(t *testing.T) {
	store := startStore(t)
	ctx := t.Context()
	table := testTable(t)

	_, err := store.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id int primary key)", table))
	require.NoError(t, err)

	txErr := fmt.Errorf("business rule violated")
	err = store.WithTx(ctx, func(tx Executor) error {
		if _, execErr := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s VALUES (1)", table)); execErr != nil {
			return execErr
		}
		return txErr
	})

	require.ErrorIs(t, err, txErr)

	var count int
	require.NoError(t, store.QueryRow(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count))
	assert.Equal(t, 0, count, "rolled-back insert must not be visible")
}

// TestWithTxRollsBackOnPanic verifies the panic path: the deferred
// rollback releases the connection (the pool stays healthy) and
// the panic still reaches the caller.
func TestWithTxRollsBackOnPanic(t *testing.T) {
	store := startStore(t)
	ctx := t.Context()
	table := testTable(t)

	_, err := store.Exec(ctx, fmt.Sprintf("CREATE TABLE %s (id int primary key)", table))
	require.NoError(t, err)

	assert.Panics(t, func() {
		_ = store.WithTx(ctx, func(tx Executor) error {
			if _, execErr := tx.Exec(ctx, fmt.Sprintf("INSERT INTO %s VALUES (1)", table)); execErr != nil {
				return execErr
			}
			panic("unexpected failure mid-transaction")
		})
	})

	var count int
	require.NoError(t, store.QueryRow(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count))
	assert.Equal(t, 0, count, "panicked transaction must not be visible")
	require.NoError(t, store.HealthCheck(ctx), "pool must recover from the aborted transaction")
}

// TestTestConnection verifies the diagnostic round trip: identity
// facts come from the server session, not just the DSN.
func TestTestConnection(t *testing.T) {
	store := startStore(t)
	ctx := t.Context()

	info, err := store.TestConnection(ctx)

	require.NoError(t, err)
	assert.Equal(t, "tango_test", info.Database)
	assert.Equal(t, "postgres", info.User)
	assert.Contains(t, info.ServerVersion, "PostgreSQL")
	assert.Greater(t, info.ServerVersionNum, int64(0))
	assert.Positive(t, info.Latency)
	assert.Greater(t, info.Stats.TotalConns(), int32(0))
}

// TestHealthCheckAndClose covers the Backend lifecycle: healthy
// while the pool is live, failing after Close drains it.
func TestHealthCheckAndClose(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	store, err := New(t.Context(), Options{DSN: pg.DSN})
	require.NoError(t, err)
	require.NoError(t, store.HealthCheck(t.Context()))

	store.Close()

	assert.Error(t, store.HealthCheck(t.Context()), "closed pool must fail health checks")
	assert.Error(t, func() error {
		_, testErr := store.TestConnection(t.Context())
		return testErr
	}(), "closed pool must fail connection tests")
}
