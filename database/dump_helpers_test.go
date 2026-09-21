package database_test

import (
	"bytes"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// Shared helpers for the dump tests. They build real databases, because the
// thing under test is the contract with Postgres: which objects a dump finds
// and whether the bytes it writes load back.

// migratedPool returns a pool over a fresh database with every migration
// applied, which is the state an export starts from and a data-only import
// expects.
func migratedPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(),
		datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)

	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := pgxpool.New(t.Context(), dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	return pool, dsn
}

// dumper builds the datastore handle the exporter and restorer take.
func dumper(t *testing.T, dsn string) *datastore.Postgres {
	t.Helper()
	store, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(store.Close)
	return store
}

// seedRows inserts one user and one password row, so a dump has something to
// carry across the three schemas.
func seedRows(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO public.users (id, username, email, first_name, last_name, display_name, is_admin)
		VALUES ('01890000-0000-7000-8000-000000000001', 'alice', 'alice@example.com',
		        'Alice', 'A', 'Alice A', true)`)
	require.NoError(t, err)
	_, err = pool.Exec(t.Context(), `
		INSERT INTO public.user_passwords (user_id, password_hash)
		VALUES ('01890000-0000-7000-8000-000000000001', '$scrypt$test')`)
	require.NoError(t, err)
}

// dump runs an export and returns the file contents.
func dump(t *testing.T, store *datastore.Postgres, opts database.DumpOptions) string {
	t.Helper()
	var out bytes.Buffer
	_, err := database.NewExporter(store, opts).Dump(t.Context(), &out, nil)
	require.NoError(t, err)
	return out.String()
}
