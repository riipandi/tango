package transport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonUnmarshal(data []byte, v any) error {
	return jsonv2.Unmarshal(data, v)
}

func testConfig() *config.Config {
	return &config.Config{
		Host: "localhost",
		Port: 3080,
		Public: config.PublicConfig{
			BaseURL: "http://localhost:3000",
		},
	}
}

// TestAPIHealthzReadiness checks the real readiness handler: every
// dependency check passes → 200 "up"; any failing check → 503
// "down" with the failing component named.
func TestAPIHealthzReadiness(t *testing.T) {
	healthy := []HealthCheck{
		{Name: "database", Check: func(ctx context.Context) error { return nil }},
	}

	w := httptest.NewRecorder()
	newHealthHandler(healthy).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var up struct {
		Status string `json:"status"`
	}
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &up))
	assert.Equal(t, "up", up.Status)

	failing := []HealthCheck{
		{Name: "database", Check: func(ctx context.Context) error { return nil }},
		{Name: "cache", Check: func(ctx context.Context) error { return errors.New("connection refused") }},
	}

	w = httptest.NewRecorder()
	newHealthHandler(failing).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	var down struct {
		Status  string `json:"status"`
		Details map[string]struct {
			Status string `json:"status"`
		} `json:"details"`
	}
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &down))
	assert.Equal(t, "down", down.Status)
	assert.Equal(t, "up", down.Details["database"].Status)
	assert.Equal(t, "down", down.Details["cache"].Status)
}

// TestRootHealthzAndVersionEndpoints checks health and version endpoints.
func TestRootHealthzAndVersionEndpoints(t *testing.T) {
	w := httptest.NewRecorder()
	RootHealthzHandler(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())
}

func TestAPIRootHandler(t *testing.T) {
	w := httptest.NewRecorder()
	APIRootHandler(w, httptest.NewRequest(http.MethodGet, "/api/", nil))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var body struct {
		Status string `json:"status"`
		Data   struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"data"`
		Metadata struct {
			StatusCode int    `json:"status_code"`
			RequestID  string `json:"request_id"`
		} `json:"metadata"`
	}
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "success", body.Status)
	assert.Equal(t, config.AppName, body.Data.Name)
	assert.Equal(t, config.AppVersion, body.Data.Version)
	assert.Equal(t, http.StatusOK, body.Metadata.StatusCode)
	assert.NotEmpty(t, body.Metadata.RequestID)
}

func TestStaticAssetsHandler(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(RouteSet{}, cfg, testLogger(), nil, nil, nil)

	// Missing files return JSON 404.
	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/missing.js", nil))
	require.Equal(t, http.StatusNotFound, w.Code)

	// Existing files are served with their MIME type.
	if _, err := os.Stat("web/output/images/logoEmail.svg"); err == nil {
		w = httptest.NewRecorder()
		srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/images/logoEmail.svg", nil))
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "image/svg+xml", w.Header().Get("Content-Type"))
	}
}
