package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/registry"
)

func contextWithTimeout() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = cancel // Shutdown completes synchronously; deadline is a safety net
	return ctx
}

func TestNewHTTPServerRoutes(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(registry.New(registry.Deps{Config: cfg}), cfg)

	cases := []struct {
		path       string
		wantStatus int
		checkJSON  bool
	}{
		{"/healthz", http.StatusOK, true},   // probes default upstream (network)
		{"/api", http.StatusOK, true},       // identity apiRoot
		{"/api/users", http.StatusOK, true}, // identity list
		{"/api/nope", http.StatusNotFound, true},
		{"/.well-known/version", http.StatusOK, true},
		{"/static/app.js", http.StatusOK, true},
		{"/some-page", http.StatusNotFound, false}, // dev static fallback
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
	srv := NewHTTPServer(registry.New(registry.Deps{Config: cfg}), cfg)

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"name":"John"}`)))

	require.Equal(t, http.StatusCreated, w.Code)

	var user map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &user))
	assert.Equal(t, "John", user["name"])
}

func TestHTTPServerShutdown(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(kernel.NewRegistry(), cfg)

	// Server never listened: Shutdown must be a safe no-op.
	assert.NoError(t, srv.Shutdown(contextWithTimeout()))
	assert.Nil(t, srv.Server)
}
