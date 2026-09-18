package datastore

import (
	"fmt"
	"strings"
	"testing"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startStore opens a test Postgres connection.
func startStore(t *testing.T) *Postgres {
	t.Helper()

	pg := testutils.StartPostgres(t.Context(), t)
	store, err := New(t.Context(), Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// testTable returns a unique SQL-safe table name.
func testTable(t *testing.T) string {
	t.Helper()

	replacer := strings.NewReplacer("/", "_", "=", "_", " ", "_", "#", "_", ".", "_")
	return fmt.Sprintf("datastore_%s", replacer.Replace(strings.ToLower(t.Name())))
}

// TestNewFailsFastOnUnreachableHost checks the construction ping.
func TestNewFailsFastOnUnreachableHost(t *testing.T) {
	store, err := New(t.Context(), Options{
		DSN: "postgresql://postgres:postgres@127.0.0.1:59999/postgres?sslmode=disable&connect_timeout=1",
	})

	require.Error(t, err)
	assert.ErrorContains(t, err, "ping 127.0.0.1")
	assert.Nil(t, store)
}

// TestPostgresExecutorRoundTrip checks the Executor operations.
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

// TestWithTxCommits checks that committed data is visible.
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

// TestWithTxRollsBackOnError checks rollback and error propagation.
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

// TestWithTxRollsBackOnPanic checks rollback and panic propagation.
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

// TestTestConnection checks details read from the server.
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

// TestHealthCheckAndClose checks health before and after Close.
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
