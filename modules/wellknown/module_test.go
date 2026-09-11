package wellknown

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

func TestWellknownEndpoints(t *testing.T) {
	mod := New().(*Module)
	r := chi.NewRouter()
	mod.Routes(r)

	cases := []struct {
		path       string
		wantStatus int
	}{
		{"/.well-known/version", http.StatusOK},
		{"/.well-known/jwks.json", http.StatusOK},
		{"/.well-known/openid-configuration", http.StatusOK},
	}

	for _, tc := range cases {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, tc.path, nil))

		assert.Equal(t, tc.wantStatus, w.Code, tc.path)
	}
}

func TestVersionEndpoint(t *testing.T) {
	mod := New().(*Module)
	r := chi.NewRouter()
	mod.Routes(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/.well-known/version", nil))

	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, config.AppName, body["name"])
	assert.Equal(t, config.AppVersion, body["version"])
}
