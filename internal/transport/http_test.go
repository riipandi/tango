package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/registry"
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

func TestNewHTTPServerRoutes(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(registry.New(registry.Deps{Config: cfg}), cfg, testLogger())

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
	srv := NewHTTPServer(registry.New(registry.Deps{Config: cfg}), cfg, testLogger())

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/users", strings.NewReader(`{"name":"John"}`)))

	require.Equal(t, http.StatusCreated, w.Code)

	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	assert.Equal(t, "success", payload.Status)
	assert.Equal(t, "John", payload.Data.Name)
}

func TestHTTPServerShutdown(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(kernel.NewRegistry(), cfg, testLogger())

	// Server never listened: Shutdown must be a safe no-op.
	assert.NoError(t, srv.Shutdown(contextWithTimeout()))
	assert.Nil(t, srv.Server)
}
