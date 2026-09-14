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
)

// testDeps builds Deps over the shared test Postgres with all
// migrations applied. Each call gets a fresh pool over the shared
// container; schema state is shared (goose tracks versions).
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
	assert.Len(t, modules, 8)

	// Queue first: its Stop drains last on shutdown.
	wantOrder := []string{"queue", "jobs", "auditlog", "identity", "appimage", "webhook", "appconfig", "federation"}
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

func TestMailersSettingsFromValues(t *testing.T) {
	fallback := config.MailerConfig{
		FromEmail: "mailer@example.com", FromName: "Env Name",
		SMTPHost: "env-host", SMTPPort: 1025,
	}

	// Empty values keep the env fallback per field.
	values := map[string]string{}
	got := MailerSettingsFromValues(values, fallback)
	assert.Equal(t, "env-host", got.SMTPHost)
	assert.Equal(t, 1025, got.SMTPPort)

	// DB overrides win, port parses, secure flips.
	got = MailerSettingsFromValues(map[string]string{
		"smtp_host": "db-host", "smtp_port": "2525", "smtp_secure": "true",
		"smtp_from_name": "DB Name",
	}, fallback)
	assert.Equal(t, "db-host", got.SMTPHost)
	assert.Equal(t, 2525, got.SMTPPort)
	assert.True(t, got.SMTPSecure)
	assert.Equal(t, "DB Name", got.FromName)

	// A bad port string is ignored, not fatal.
	got = MailerSettingsFromValues(map[string]string{"smtp_port": "soon"}, fallback)
	assert.Equal(t, 1025, got.SMTPPort)
}

func TestMapLDAPSettingsReadsMergedValues(t *testing.T) {
	settings := mapLDAPSettings(map[string]string{
		"ldap_enabled":       "true",
		"ldap_url":           "ldaps://directory.example",
		"ldap_bind_dn":       "cn=sync",
		"ldap_bind_password": "secret",
		"ldap_base":          "dc=example,dc=org",
	})
	assert.True(t, settings.Enabled)
	assert.Equal(t, "ldaps://directory.example", settings.URL)
	assert.Equal(t, "cn=sync", settings.BindDN)
	assert.Equal(t, "secret", settings.BindPassword)
	assert.Equal(t, "dc=example,dc=org", settings.Base)

	// Bool parsing is lenient: anything but "true" is off.
	assert.False(t, mapLDAPSettings(map[string]string{"ldap_enabled": "1"}).Enabled)
}

func TestUserCorePersistsInPostgres(t *testing.T) {
	deps := testDeps(t)
	New(deps) // wiring builds without panics against the real database

	// Unique per run: the test container may be shared.
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
