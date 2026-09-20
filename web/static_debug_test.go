//go:build !release

package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func TestSetupStaticJSONFallbacks(t *testing.T) {
	r := chi.NewRouter()
	SetupStatic(r)

	for _, path := range []string{"/api/missing", "/.well-known/missing", "/static/missing.js"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))

		assert.Equal(t, http.StatusNotFound, w.Code, path)
		assert.Contains(t, w.Body.String(), "not found", path)
	}
}

func TestSetupStaticPageFallback(t *testing.T) {
	r := chi.NewRouter()
	SetupStatic(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/some-page", nil))

	assert.Equal(t, http.StatusNotFound, w.Code)
}
