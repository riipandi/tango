package transport

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
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

func TestHealthzLiveness(t *testing.T) {
	w := httptest.NewRecorder()
	HealthCheckHandler(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "healthy", body["status"])
}

// TestRootHealthzAndVersionEndpoints checks health and version endpoints.
func TestRootHealthzAndVersionEndpoints(t *testing.T) {
	w := httptest.NewRecorder()
	RootHealthzHandler(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	require.Equal(t, http.StatusNoContent, w.Code)
	require.Empty(t, w.Body.String())

	w = httptest.NewRecorder()
	VersionCurrentHandler(w, httptest.NewRequest(http.MethodGet, "/api/version/current", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var current map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &current))
	assert.Equal(t, config.AppVersion, current["version"])

	w = httptest.NewRecorder()
	VersionLatestHandler(nil)(w, httptest.NewRequest(http.MethodGet, "/api/version/latest", nil))
	require.Equal(t, http.StatusOK, w.Code)
	var latest map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &latest))
	assert.Equal(t, config.AppVersion, latest["version"])
}

// stubLatestVersionSource supplies a fixed cached release.
type stubLatestVersionSource struct{ version string }

func (s stubLatestVersionSource) Latest() string { return s.version }

func TestVersionLatestUsesFeed(t *testing.T) {
	w := httptest.NewRecorder()
	VersionLatestHandler(stubLatestVersionSource{version: "9.9.9"})(w, httptest.NewRequest(http.MethodGet, "/api/version/latest", nil))
	require.Equal(t, http.StatusOK, w.Code)

	var latest map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &latest))
	assert.Equal(t, "9.9.9", latest["version"])
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
	srv := NewHTTPServer(kernel.NewRegistry(), cfg, testLogger(), nil, nil)

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
