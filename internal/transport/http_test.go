package transport

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/account"
	"github.com/riipandi/tango/modules/identity/user"
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

// testServer builds the HTTP server over Postgres-backed modules.
func testServer(t *testing.T, cfg *config.Config) *HTTPServer {
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
	)
	return NewHTTPServer(RouteSet{
		MountAPI: func(r chi.Router) {
			audit.APIRoutes(r)
			idModule.APIRoutes(r, identity.RouteGroups{})
		},
	}, cfg, testLogger(), nil, nil)
}

func TestNewHTTPServerRoutes(t *testing.T) {
	cfg := testConfig()
	srv := testServer(t, cfg)

	cases := []struct {
		path       string
		wantStatus int
		checkJSON  bool
	}{
		{"/api/healthz", http.StatusOK, true}, // moved under the /api group
		{"/api", http.StatusOK, true},         // identity apiRoot
		{"/api/users", http.StatusOK, true},   // identity list
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

func TestNewHTTPServerMountsModules(t *testing.T) {
	cfg := testConfig()
	srv := testServer(t, cfg)

	// Use a unique username because the container may be shared.
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	body := fmt.Sprintf(`{"username":"transport_%s","email":"transport-%s@example.com"}`, stamp, stamp)

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(body)))

	require.Equal(t, http.StatusCreated, w.Code)

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "success", payload.Status)
	assert.True(t, strings.HasPrefix(payload.Data.Username, "transport_"))
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
	srv := NewHTTPServer(RouteSet{}, cfg, testLogger(), nil, nil)

	// Shutdown is safe before the server listens.
	assert.NoError(t, srv.Shutdown(contextWithTimeout()))
	assert.Nil(t, srv.Server)
}
