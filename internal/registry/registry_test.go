package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		Config: &config.Config{
			Queue:   config.QueueConfig{Workers: 2, ReleaseAfter: 30, CleanupInterval: 3600},
			Storage: config.StorageConfig{DataDir: t.TempDir()},
		},
	}
}

func TestNewRegistersAllModules(t *testing.T) {
	reg := New(testDeps(t))

	modules := reg.Modules()
	assert.Len(t, modules, 7)

	// Modules register in a fixed order.
	wantOrder := []string{"queue", "jobs", "auditlog", "identity", "webhook", "appconfig", "federation"}
	for i, want := range wantOrder {
		assert.Equal(t, want, modules[i].Name())
	}

	assert.NotNil(t, reg.Get("identity"))
	assert.NotNil(t, reg.Get("federation"))
	assert.NotNil(t, reg.Get("webhook"))
}

func TestNewRequiresDatabase(t *testing.T) {
	require.Panics(t, func() { New(Deps{}) })
}

// The LDAP sync endpoint is an excluded upstream feature; the route
// must stay unmounted even though application configuration is served.
func TestLDAPSyncEndpointNotMounted(t *testing.T) {
	reg := New(testDeps(t))
	api := chi.NewRouter()
	reg.ApplyAPI(api)

	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestUserCorePersistsInPostgres(t *testing.T) {
	deps := testDeps(t)
	New(deps) // wiring builds without panics against the real database

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
