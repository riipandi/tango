package datastore_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

func newTestPostgres(t *testing.T) *datastore.Postgres {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	pg, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             container.DSN,
		ApplicationName: "tango-test",
	})
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	return pg
}

func TestNewPostgresRequiresDSN(t *testing.T) {
	_, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{})
	require.ErrorIs(t, err, datastore.ErrMissingDSN)
}

func TestNewPostgresRejectsMinAboveMax(t *testing.T) {
	_, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:      "postgres://postgres:postgres@127.0.0.1:1/postgres",
		MinConns: 5,
		MaxConns: 2,
	})
	require.Error(t, err)
}

func TestNewPostgresFailsWhenUnreachable(t *testing.T) {
	_, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN: "postgres://postgres:postgres@127.0.0.1:1/postgres?sslmode=disable",
	})
	require.Error(t, err)
}

// scratchTable creates a uniquely named table and drops it on cleanup. A temp
// table is not usable here: the pool may serve the next statement on another
// connection, which would not see it.
func scratchTable(t *testing.T, pg *datastore.Postgres, columns string) string {
	t.Helper()

	name := fmt.Sprintf("scratch_%d", rand.Int64())
	_, err := pg.Exec(t.Context(), fmt.Sprintf("CREATE TABLE %s (%s)", name, columns))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := pg.Exec(context.WithoutCancel(t.Context()), "DROP TABLE IF EXISTS "+name)
		assert.NoError(t, err)
	})
	return name
}

func TestPostgresQueryAndTransaction(t *testing.T) {
	pg := newTestPostgres(t)
	ctx := t.Context()

	require.NoError(t, pg.Ping(ctx))

	table := scratchTable(t, pg, "id int PRIMARY KEY, name text NOT NULL")

	// Committed work is visible outside the transaction.
	err := pg.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		_, execErr := tx.Exec(ctx, "INSERT INTO "+table+" (id, name) VALUES ($1, $2)", 1, "kept")
		return execErr
	})
	require.NoError(t, err)

	var name string
	require.NoError(t, pg.QueryRow(ctx, "SELECT name FROM "+table+" WHERE id = $1", 1).Scan(&name))
	assert.Equal(t, "kept", name)

	// A failing callback rolls the whole transaction back.
	sentinel := errors.New("boom")
	err = pg.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		_, execErr := tx.Exec(ctx, "INSERT INTO "+table+" (id, name) VALUES ($1, $2)", 2, "dropped")
		if execErr != nil {
			return execErr
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	var count int
	require.NoError(t, pg.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
	assert.Equal(t, 1, count)

	// A panicking callback must not leak the transaction.
	assert.Panics(t, func() {
		_ = pg.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
			_, _ = tx.Exec(ctx, "INSERT INTO "+table+" (id, name) VALUES ($1, $2)", 3, "panic")
			panic("boom")
		})
	})

	require.NoError(t, pg.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&count))
	assert.Equal(t, 1, count, "the panicking transaction must be rolled back")
}

func TestPostgresStatsExposePoolCounters(t *testing.T) {
	pg := newTestPostgres(t)

	require.NoError(t, pg.Ping(t.Context()))
	assert.Equal(t, int32(10), pg.Stats().MaxConns())
}

func TestPostgresAppliesSessionDefaults(t *testing.T) {
	pg := newTestPostgres(t)
	ctx := t.Context()

	var searchPath, timezone, applicationName string
	err := pg.QueryRow(ctx,
		"SELECT current_setting('search_path'), current_setting('timezone'), current_setting('application_name')",
	).Scan(&searchPath, &timezone, &applicationName)
	require.NoError(t, err)

	assert.Equal(t, datastore.PgSearchPath, searchPath)
	assert.Equal(t, datastore.PgTimezone, timezone)
	assert.Equal(t, "tango-test", applicationName)
}

// Session parameters must survive connection reuse. pgxpool resets a recycled
// connection from RuntimeParams; a startup query would be lost here.
func TestPostgresKeepsSessionDefaultsOnReusedConnection(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	pg, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:      container.DSN,
		MaxConns: 1,
		MinConns: 1,
	})
	require.NoError(t, err)
	t.Cleanup(pg.Close)
	ctx := t.Context()

	var timezone string
	for range 3 {
		require.NoError(t, pg.QueryRow(ctx, "SELECT current_setting('timezone')").Scan(&timezone))
		assert.Equal(t, datastore.PgTimezone, timezone)
	}
}

func TestErrNoRowsMatchesPgx(t *testing.T) {
	pg := newTestPostgres(t)
	table := scratchTable(t, pg, "id int")

	err := pg.QueryRow(t.Context(), "SELECT id FROM "+table).Scan(new(int))
	require.ErrorIs(t, err, datastore.ErrNoRows)
}

func TestAcquireReturnsReusableConnection(t *testing.T) {
	pg := newTestPostgres(t)
	ctx := t.Context()

	conn, err := pg.Acquire(ctx)
	require.NoError(t, err)

	// A session-level setting made through the acquired connection is visible
	// on that connection only, which is what the callers need it for.
	_, err = conn.Exec(ctx, "SET application_name = 'acquired'")
	require.NoError(t, err)

	var name string
	require.NoError(t, conn.QueryRow(ctx, "SELECT current_setting('application_name')").Scan(&name))
	assert.Equal(t, "acquired", name)

	conn.Release()
	assert.Equal(t, int32(0), pg.Stats().AcquiredConns(), "the connection must return to the pool")
}

func TestMigrationDBUsesSingleConnection(t *testing.T) {
	pg := newTestPostgres(t)
	ctx := t.Context()

	db, err := pg.MigrationDB(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	stats := db.Stats()
	assert.Equal(t, 1, stats.MaxOpenConnections, "migrations must not run on a pool")

	// The migration handle is independent of the pool: closing it must not
	// affect the application pool.
	require.NoError(t, db.PingContext(ctx))
	require.NoError(t, pg.Ping(ctx))
}

func TestOpenMigrationDBWithoutPool(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)

	db, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: container.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	assert.Equal(t, 1, db.Stats().MaxOpenConnections)

	var one int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT 1").Scan(&one))
	assert.Equal(t, 1, one)
}

func TestOpenMigrationDBRequiresDSN(t *testing.T) {
	_, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{})
	require.ErrorIs(t, err, datastore.ErrMissingDSN)
}
