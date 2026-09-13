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

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
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
	// Silent mock: same API, emits nothing, Fatal does not exit.
	return loglayer.NewMock()
}

// testServer builds the HTTP server over real Postgres-backed
// modules (shared test container) — the same wiring the registry
// uses in production.
func testServer(t *testing.T, cfg *config.Config) *HTTPServer {
	pg := testutils.StartPostgres(t.Context(), t)
	if _, err := database.MigrateUp(t.Context(), pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	db, err := datastore.New(t.Context(), datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	reg := kernel.NewRegistry()
	reg.Register(auditlog.New(auditlog.NewPostgresStore(db)))
	reg.Register(identity.New(user.NewService(user.NewPostgresStore(db), nil)))
	return NewHTTPServer(reg, cfg, testLogger(), nil)
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
		{"/static/app.js", http.StatusOK, true},
		{"/some-page", spaFallbackStatus, false}, // dev: 404, release: SPA shell
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

	// Unique username per run: the test container is shared.
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

	// No incoming header: the server generates one and echoes it.
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api", nil))
	require.Equal(t, http.StatusOK, w.Code)
	id := w.Header().Get("X-Request-Id")
	assert.NotEmpty(t, id)
	assert.True(t, strings.HasPrefix(id, "req_"), id)

	// Incoming header: echoed verbatim.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api", nil)
	req.Header.Set("X-Request-Id", "client-supplied-id")
	srv.Router.ServeHTTP(w, req)
	assert.Equal(t, "client-supplied-id", w.Header().Get("X-Request-Id"))
}

func TestHTTPServerShutdown(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(kernel.NewRegistry(), cfg, testLogger(), nil)

	// Server never listened: Shutdown must be a safe no-op.
	assert.NoError(t, srv.Shutdown(contextWithTimeout()))
	assert.Nil(t, srv.Server)
}
