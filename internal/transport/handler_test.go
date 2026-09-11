package transport

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
)

func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func testConfig() *config.Config {
	return &config.Config{
		Host: "localhost",
		Port: 3080,
		Public: config.PublicConfig{
			BaseURL:        "http://localhost:3000",
			HealthcheckURL: "https://api.ipify.org",
		},
	}
}

func TestHealthzHealthy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("1.2.3.4"))
	}))
	defer upstream.Close()

	cfg := testConfig()
	cfg.Public.HealthcheckURL = upstream.URL

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	HealthzHandler(cfg)(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "healthy", body["status"])
	assert.Equal(t, "1.2.3.4", body["ip_address"])
}

func TestHealthzUpstreamNonSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	cfg := testConfig()
	cfg.Public.HealthcheckURL = upstream.URL

	w := httptest.NewRecorder()
	HealthzHandler(cfg)(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body map[string]string
	require.NoError(t, jsonUnmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "unhealthy", body["status"])
}

func TestHealthzUpstreamUnreachable(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	upstreamURL := upstream.URL
	upstream.Close() // guarantee connection failure

	cfg := testConfig()
	cfg.Public.HealthcheckURL = upstreamURL

	w := httptest.NewRecorder()
	HealthzHandler(cfg)(w, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestStaticAssetsHandler(t *testing.T) {
	cfg := testConfig()
	srv := NewHTTPServer(kernel.NewRegistry(), cfg)

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "app.js", body["path"])
}
