package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
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

func TestHealthzLiveness(t *testing.T) {
	w := httptest.NewRecorder()
	HealthzHandler(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "healthy", body["status"])
}

func TestAPIRootHandler(t *testing.T) {
	w := httptest.NewRecorder()
	APIRootHandler(w, httptest.NewRequest(http.MethodGet, "/api/", nil))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")

	var body map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, config.AppName, body["name"])
	assert.Equal(t, config.AppVersion, body["version"])
}

func TestStaticAssetsHandler(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(kernel.NewRegistry(), cfg, testLogger())

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "app.js", body["path"])
}
