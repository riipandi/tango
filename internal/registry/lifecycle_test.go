package registry_test

import (
	"log/slog"
	"testing"

	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/pkg/testutils"
)

// servedInjector builds the container over a fresh migrated database, the
// state a serve run reaches before the listener opens: the queue's
// construction seeds its jobs, which reads the database, so a real one backs
// the walk.
func servedInjector(t *testing.T) *do.RootScope {
	t.Helper()

	dsn := testutils.StartPostgres(t.Context(), t).NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	cfg := config.Default()
	cfg.Database.URL = dsn
	cfg.Storage.Watch.Enable = true

	injector := registry.New(t.Context(), cfg, nil, slog.New(slog.DiscardHandler))
	t.Cleanup(func() { injector.Shutdown() })
	return injector
}

// TestPrewarmResolvesEveryBlockingService is the warm-up contract: a run with
// a database that answers prewarms clean, and the router behind the server is
// built — the areas' Mount ran — by the time it returns.
func TestPrewarmResolvesEveryBlockingService(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	injector := servedInjector(t)

	require.NoError(t, registry.Prewarm(injector))
}

// TestRunnersAreTheLongRunningComponentsInStartOrder pins the list the serve
// run starts and drains: the queue first, the scheduler second, the staging
// watcher last and only when it is enabled.
func TestRunnersAreTheLongRunningComponentsInStartOrder(t *testing.T) {
	testutils.SkipWithoutDocker(t)

	injector := servedInjector(t)

	runners, err := registry.Runners(injector)
	require.NoError(t, err)

	names := make([]string, 0, len(runners))
	for _, runner := range runners {
		names = append(names, runner.Name)
	}
	assert.Equal(t, []string{"queue", "scheduler", "staging watch"}, names)

	// The drain ownership travels with the list: the queue outlives the
	// listener, so the container's shutdown walk drains it; the scheduler
	// stops in the run's own drain window.
	assert.Nil(t, runners[0].Stop, "the queue's drain belongs to the injector")
	assert.NotNil(t, runners[1].Stop, "the scheduler drains inside the shutdown window")
	assert.Nil(t, runners[2].Stop, "the watcher ends with the run's context")
}
