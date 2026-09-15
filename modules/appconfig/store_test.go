package appconfig

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// newStoreStack boots a throwaway Postgres, applies migrations, and
// returns the real store wired into the module.
func newStoreStack(t *testing.T) (Store, *Module) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	store := NewPostgresStore(ds)
	return store, func() *Module {
		m := New(nil).WithStore(store)
		m.UseGuard(testPrincipalContext)
		return m
	}()
}

// TestConfigCRUD covers the public/admin views plus the PUT round
// trip: defaults fold with overrides, validation rejects bad values,
// unknown keys are ignored.
func TestConfigCRUD(t *testing.T) {
	_, module := newStoreStack(t)
	router := mount(t, module)

	// Public view (no guard): defaults only, public keys.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/application-configuration", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var public struct {
		Data []variable `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &public))
	require.NotEmpty(t, public.Data)
	for _, v := range public.Data {
		assert.False(t, strings.HasPrefix(v.Key, "smtp"), "env-only keys never leak: %s", v.Key)
	}

	// Admin /all includes private keys with the is_public flag.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/application-configuration/all", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var all struct {
		Data []variable `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &all))
	require.NotEmpty(t, all.Data)

	names := map[string]bool{}
	for _, v := range all.Data {
		names[v.Key] = v.IsPublic
	}
	assert.True(t, names["app_name"], "public key visible")
	assert.False(t, names["session_duration"], "private key present but flagged")

	// PUT updates one key and echoes the full view.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/application-configuration",
		strings.NewReader(`{"app_name":"Tango","allow_user_signups":"open","no_such_key":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var updated struct {
		Data []variable `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &updated))
	values := map[string]string{}
	for _, v := range updated.Data {
		values[v.Key] = v.Value
	}
	assert.Equal(t, "Tango", values["app_name"])
	assert.Equal(t, "open", values["allow_user_signups"])
	assert.NotContains(t, values, "no_such_key")

	// The override survives a re-read of the public view.
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/application-configuration", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &public))
	found := false
	for _, v := range public.Data {
		if v.Key == "app_name" {
			found = true
			assert.Equal(t, "Tango", v.Value)
		}
	}
	assert.True(t, found, "app_name in public view")

	// Bad enum + bad int → 422.
	for _, body := range []string{
		`{"allow_user_signups":"sometimes"}`,
		`{"session_duration":"soon"}`,
		`{"webauthn_user_verification":"optional"}`,
	} {
		w = httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPut, "/api/application-configuration", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnprocessableEntity, w.Code, body)
	}
}

// TestEnvDefaultsAndSensitiveRedaction covers the SMTP keys:
// env-provided defaults fold under DB overrides and sensitive values
// never leave the server, while MergedValues still serves the real
// secret to wired consumers.
func TestEnvDefaultsAndSensitiveRedaction(t *testing.T) {
	_, module := newStoreStack(t)
	module = module.WithEnvDefaults(map[string]string{
		"smtp_host":     "relay.example",
		"smtp_password": "env-secret",
	})
	router := mount(t, module)
	ctx := t.Context()

	readAll := func() map[string]string {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/application-configuration/all", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var all struct {
			Data []variable `json:"data"`
		}
		require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &all))
		values := map[string]string{}
		for _, v := range all.Data {
			values[v.Key] = v.Value
		}
		return values
	}

	// Env layer sits above the catalog defaults; the env password is
	// still redacted in the admin view.
	values := readAll()
	assert.Equal(t, "relay.example", values["smtp_host"])
	assert.Equal(t, "", values["smtp_password"], "sensitive values redact even from env")

	// Real values reach consumers through MergedValues.
	merged, err := module.MergedValues(ctx)
	require.NoError(t, err)
	assert.Equal(t, "relay.example", merged["smtp_host"])
	assert.Equal(t, "env-secret", merged["smtp_password"])

	// DB overrides top the env layer; the stored secret stays hidden.
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/application-configuration",
		strings.NewReader(`{"smtp_host":"db-relay.example","smtp_password":"db-secret"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	values = readAll()
	assert.Equal(t, "db-relay.example", values["smtp_host"])
	assert.Equal(t, "", values["smtp_password"])

	merged, err = module.MergedValues(ctx)
	require.NoError(t, err)
	assert.Equal(t, "db-secret", merged["smtp_password"])
	assert.Equal(t, "db-relay.example", merged["smtp_host"])
}
