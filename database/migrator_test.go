package database_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// migrationCount is the number of files in database/migrations.
const migrationCount = 9

// highestVersion is the version of the last migration file.
const highestVersion = 9

func newMigrator(t *testing.T) (*database.Migrator, *sql.DB) {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	db, err := datastore.OpenMigrationDB(t.Context(),
		datastore.PostgresOptions{DSN: container.NewDatabase(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	migrator, err := database.NewMigrator(t.Context(), db, database.MigratorOptions{})
	require.NoError(t, err)
	return migrator, db
}

func TestNewMigratorRequiresHandle(t *testing.T) {
	_, err := database.NewMigrator(t.Context(), nil, database.MigratorOptions{})
	require.Error(t, err)
}

// Every embedded file must be picked up. goose silently skips a file whose
// numeric prefix is below 1, which would make a migration never run.
func TestMigratorLoadsEveryEmbeddedFile(t *testing.T) {
	migrator, _ := newMigrator(t)

	assert.Equal(t, int64(highestVersion), migrator.HighestVersion())
}

func TestMigratorAppliesEveryMigration(t *testing.T) {
	migrator, db := newMigrator(t)
	ctx := t.Context()

	applied, err := migrator.Up(ctx)
	require.NoError(t, err)
	require.Len(t, applied, migrationCount)
	for i, migration := range applied {
		assert.Equal(t, int64(i+1), migration.Version, "migrations must apply in version order")
		assert.NotEmpty(t, migration.Name)
		assert.False(t, migration.Empty)
	}

	version, err := migrator.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(highestVersion), version)

	// The schema the first migration creates must exist, proving version 1 ran.
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'reference')").Scan(&exists))
	assert.True(t, exists, "migration 00001 must have created the reference schema")

	// Second run is a no-op.
	again, err := migrator.Up(ctx)
	require.NoError(t, err)
	assert.Empty(t, again)
}

func TestMigratorRecordsVersionInAppMigration(t *testing.T) {
	migrator, db := newMigrator(t)
	ctx := t.Context()

	_, err := migrator.Up(ctx)
	require.NoError(t, err)

	var table string
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT table_name FROM information_schema.tables WHERE table_name = $1",
		database.VersionTable).Scan(&table))
	assert.Equal(t, "app_migration", table)

	var count int
	require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM "+database.VersionTable).Scan(&count))
	// One row per applied migration plus goose's version 0 sentinel.
	assert.Equal(t, migrationCount+1, count)
}

func TestMigratorStatusAndPending(t *testing.T) {
	migrator, _ := newMigrator(t)
	ctx := t.Context()

	statuses, err := migrator.Status(ctx)
	require.NoError(t, err)
	require.Len(t, statuses, migrationCount)
	for _, status := range statuses {
		assert.False(t, status.Applied)
		assert.True(t, status.AppliedAt.IsZero())
	}

	pending, err := migrator.Pending(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, migrationCount)

	_, err = migrator.Up(ctx)
	require.NoError(t, err)

	statuses, err = migrator.Status(ctx)
	require.NoError(t, err)
	for _, status := range statuses {
		assert.True(t, status.Applied)
		assert.False(t, status.AppliedAt.IsZero())
	}

	pending, err = migrator.Pending(ctx)
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestMigratorUpToStopsAtVersion(t *testing.T) {
	migrator, db := newMigrator(t)
	ctx := t.Context()

	applied, err := migrator.UpTo(ctx, 3)
	require.NoError(t, err)
	require.Len(t, applied, 3)
	assert.Equal(t, int64(3), applied[len(applied)-1].Version)

	version, err := migrator.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(3), version)

	// The queue tables arrive in a later migration, so they must not exist yet.
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'queue_tasks')").Scan(&exists))
	assert.False(t, exists, "migration 00008 must not have run")

	rest, err := migrator.UpTo(ctx, highestVersion)
	require.NoError(t, err)
	assert.Len(t, rest, migrationCount-3)
}

func TestMigratorDownRollsBackNewestFirst(t *testing.T) {
	migrator, db := newMigrator(t)
	ctx := t.Context()

	_, err := migrator.Up(ctx)
	require.NoError(t, err)

	rolled, err := migrator.Down(ctx, 2)
	require.NoError(t, err)
	require.Len(t, rolled, 2)
	assert.Equal(t, int64(9), rolled[0].Version)
	assert.Equal(t, int64(8), rolled[1].Version)

	version, err := migrator.Version(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(7), version)

	// 00009 adds the remember column and 00008 creates queue_tasks; both must be
	// gone, while the tables from earlier migrations stay.
	var exists bool
	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'sessions' AND column_name = 'remember')").Scan(&exists))
	assert.False(t, exists)

	require.NoError(t, db.QueryRowContext(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'queue_tasks')").Scan(&exists))
	assert.False(t, exists)

	// The applied rows are gone too, so a later up reapplies them.
	pending, err := migrator.Pending(ctx)
	require.NoError(t, err)
	assert.Len(t, pending, 2)
}

// A count above what is applied rolls back everything and reports only what it
// actually rolled back.
func TestMigratorDownStopsWhenDatabaseIsEmpty(t *testing.T) {
	migrator, _ := newMigrator(t)
	ctx := t.Context()

	_, err := migrator.UpTo(ctx, 2)
	require.NoError(t, err)

	rolled, err := migrator.Down(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, rolled, 2)

	version, err := migrator.Version(ctx)
	require.NoError(t, err)
	assert.Zero(t, version, "the goose sentinel row keeps the version at 0")

	rolled, err = migrator.Down(ctx, 1)
	require.NoError(t, err)
	assert.Empty(t, rolled, "an empty database is not an error")
}

func TestMigratorDownRejectsZeroCount(t *testing.T) {
	migrator, _ := newMigrator(t)

	_, err := migrator.Down(t.Context(), 0)
	require.Error(t, err)
}

func TestMigratorAppliedIsNewestFirst(t *testing.T) {
	migrator, _ := newMigrator(t)
	ctx := t.Context()

	applied, err := migrator.Applied(ctx)
	require.NoError(t, err)
	assert.Empty(t, applied)

	_, err = migrator.UpTo(ctx, 3)
	require.NoError(t, err)

	applied, err = migrator.Applied(ctx)
	require.NoError(t, err)
	require.Len(t, applied, 3)
	assert.Equal(t, int64(3), applied[0].Version)
	assert.Equal(t, int64(1), applied[2].Version)
	for _, status := range applied {
		assert.True(t, status.Applied)
	}
}

// Migrations run on one pinned connection: a multi-statement file would break if
// goose held the advisory lock on a different backend than the statements.
func TestMigratorUsesSingleConnection(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	db, err := datastore.OpenMigrationDB(t.Context(),
		datastore.PostgresOptions{DSN: container.NewDatabase(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	assert.Equal(t, 1, db.Stats().MaxOpenConnections)

	migrator, err := database.NewMigrator(t.Context(), db, database.MigratorOptions{})
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Len(t, applied, migrationCount)
}

func TestMigratorAllowsOutOfOrderWhenEnabled(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	db, err := datastore.OpenMigrationDB(t.Context(),
		datastore.PostgresOptions{DSN: container.NewDatabase(t)})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	migrator, err := database.NewMigrator(t.Context(), db,
		database.MigratorOptions{AllowOutOfOrder: true})
	require.NoError(t, err)

	applied, err := migrator.Up(t.Context())
	require.NoError(t, err)
	assert.Len(t, applied, migrationCount)
}
