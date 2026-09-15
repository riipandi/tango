package registry

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.loglayer.dev/v3"
)

// testDeps builds Deps over migrated test Postgres.
func testDeps(t *testing.T) Deps {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	return Deps{
		DB: store,
		// Silent logger: queue and job logs flow here at Start.
		Logger: loglayer.NewMock(),
		Config: &config.Config{
			Queue:   config.QueueConfig{Workers: 2, ReleaseAfter: 30, CleanupInterval: 3600},
			Storage: config.StorageConfig{DataDir: t.TempDir()},
		},
	}
}

func TestNewBuildsAllModules(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	assert.NotNil(t, rt.Queue)
	assert.NotNil(t, rt.Jobs)
	assert.NotNil(t, rt.AuditLog)
	assert.NotNil(t, rt.Identity)
	assert.NotNil(t, rt.Webhook)
	assert.NotNil(t, rt.AppConfig)
	assert.NotNil(t, rt.Federation)
}

func TestNewRequiresDatabase(t *testing.T) {
	_, err := New(Deps{})
	assert.ErrorContains(t, err, "nil database store")
}

func TestUserCorePersistsInPostgres(t *testing.T) {
	deps := testDeps(t)
	_, err := New(deps) // wiring builds without errors against the real database
	require.NoError(t, err)

	// Use a unique value because the container may be shared.
	unique := strconv.FormatInt(time.Now().UnixNano(), 10)

	svc := user.NewService(user.NewPostgresStore(deps.DB), nil)
	created, err := svc.Create(context.Background(), user.CreateParams{
		Username: "rt_" + unique,
		Email:    "registrytest-" + unique + "@example.com",
	})
	require.NoError(t, err)
	assert.False(t, created.ID.IsZero())

	fetched, err := svc.GetByID(context.Background(), created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.Email, fetched.Email)
}
