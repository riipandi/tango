package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/modules/admin/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/account"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.loglayer.dev/v3"
)

func contextWithTimeout() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // Shutdown completes synchronously; deadline is a safety net
	return ctx
}

func testLogger() logger.Logger {
	// Use a silent logger for the test server.
	return loglayer.NewMock()
}

// testServer builds the HTTP server over Postgres-backed modules;
// session-guarded routes see the denying guard unless overridden.
func testServer(t *testing.T, cfg *config.Config) *HTTPServer {
	return newTestServer(t, cfg, denyAllGuard)
}

func newTestServer(t *testing.T, cfg *config.Config, guard kernel.Guard) *HTTPServer {
	pg := testutils.StartPostgres(t.Context(), t)
	if _, err := database.MigrateUp(t.Context(), pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := datastore.New(t.Context(), datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	audit := auditlog.New(auditlog.NewPostgresStore(db))
	core := user.NewService(user.NewPostgresStore(db), nil)
	idModule := identity.New(
		core,
		account.NewService(user.NewPostgresStore(db), nil, nil, nil),
		nil,                     // sessions: self-service surfaces stay unmounted
		nil,                     // groups
		nil,                     // claims
		nil, nil, nil, nil, nil, // optional features
		nil, // api-access
		nil, // api-keys
		nil, // password recovery
		nil, // TOTP MFA
	)
	return NewHTTPServer(RouteSet{
		MountAPI: func(r chi.Router) {
			audit.APIRoutes(r)
			idModule.APIRoutes(r, identity.RouteGroups{})
		},
		RequireSession: guard,
	}, cfg, testLogger(), nil, nil, nil)
}

// denyAllGuard rejects every request with the standard 401 envelope,
// standing in for an absent session.
func denyAllGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
	})
}

func TestNewHTTPServerRoutes(t *testing.T) {
	cfg := testConfig()
	srv := testServer(t, cfg)

	cases := []struct {
		path       string
		wantStatus int
		checkJSON  bool
	}{
		{"/api/healthz", http.StatusOK, true},                   // moved under the /api group
		{"/api", http.StatusOK, true},                           // identity apiRoot
		{"/api/users", http.StatusNotFound, true},               // users cutover: ConnectRPC only
		{"/api/version/current", http.StatusUnauthorized, true}, // session-guarded
		{"/api/version/latest", http.StatusOK, true},            // public release feed
		{"/api/nope", http.StatusNotFound, true},
		{"/.well-known/version", http.StatusOK, true},
		{"/static/missing.js", http.StatusNotFound, true}, // static 404 is JSON
		{"/some-page", spaFallbackStatus, false},          // dev: 404, release: SPA shell
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))
		assert.Equal(t, tc.wantStatus, w.Code, tc.path)

		if tc.checkJSON {
			assert.Contains(t, w.Header().Get("Content-Type"), "application/json", tc.path)
		}
	}
}

func TestVersionContracts(t *testing.T) {
	cfg := testConfig()

	// The release feed is public and cacheable.
	srv := testServer(t, cfg)
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/version/latest", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "public, max-age=300, stale-while-revalidate=900", w.Header().Get("Cache-Control"))
	assert.NotEmpty(t, versionData(t, w).LatestVersion)

	// The deployed version requires a session; a signed-in request
	// passes the guard and reads the build version.
	srv = newTestServer(t, cfg, allowAllGuard)
	w = httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/version/current", nil))
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotEmpty(t, versionData(t, w).CurrentVersion)
}

func allowAllGuard(next http.Handler) http.Handler {
	return next
}

func TestNewHTTPServerMountsModules(t *testing.T) {
	cfg := testConfig()
	srv := testServer(t, cfg)

	// The module mount is exercised through the retained bare-bytes
	// user route; the CRUD surfaces answer Connect 404 documents.
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users/user_01m2v03pcxe5m850f9098vgaeq/profile-picture.png", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader("{}")))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRequestIDMiddleware(t *testing.T) {
	cfg := testConfig()
	srv := testServer(t, cfg)

	// The server generates an ID when none is supplied.
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api", nil))
	require.Equal(t, http.StatusOK, w.Code)
	id := w.Header().Get("X-Request-Id")
	assert.NotEmpty(t, id)
	assert.True(t, strings.HasPrefix(id, "req_"), id)

	// A supplied ID is echoed unchanged.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Request-Id", "client-supplied-id")
	srv.Router.ServeHTTP(w, req)
	assert.Equal(t, "client-supplied-id", w.Header().Get("X-Request-Id"))
}

func TestHTTPServerShutdown(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(RouteSet{}, cfg, testLogger(), nil, nil, nil)

	// Shutdown is safe before the server listens.
	assert.NoError(t, srv.Shutdown(contextWithTimeout()))
	assert.Nil(t, srv.Server)
}
