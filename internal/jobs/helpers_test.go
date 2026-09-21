package jobs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/queue"
)

// migratedClient applies the migrations to a fresh test database and returns
// the pool and a client on it, without starting the dispatcher.
func migratedClient(t *testing.T, dsn string) (*datastore.Postgres, *queue.Client) {
	t.Helper()

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             dsn,
		ApplicationName: "queue_test",
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	client, err := queue.NewClient(queue.ClientConfig{
		Store:        pool,
		NumWorkers:   2,
		ReleaseAfter: 10 * time.Second,
	})
	require.NoError(t, err)
	return pool, client
}
