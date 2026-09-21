package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

func TestCORSPreflightNamesTheAllowedOrigin(t *testing.T) {
	handle := CORS(config.CORS{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET", "POST"},
		AllowedHeaders: []string{"Authorization"},
		MaxAge:         time.Hour,
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/healthz", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec := httptest.NewRecorder()

	handle(http.NotFoundHandler()).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNoContent, rec.Code,
		"a preflight is answered with 204, the status rs/cors replies with")
	assert.Equal(t, "http://localhost:3000", rec.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, rec.Header().Get("Access-Control-Allow-Methods"), "POST")
	assert.NotEmpty(t, rec.Header().Get("Access-Control-Max-Age"))
}

func TestCORSLeavesAForeignOriginUnanswered(t *testing.T) {
	handle := CORS(config.CORS{
		AllowedOrigins: []string{"http://localhost:3000"},
		AllowedMethods: []string{"GET"},
	})

	req := httptest.NewRequest(http.MethodOptions, "/api/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Access-Control-Request-Method", "GET")
	rec := httptest.NewRecorder()

	handle(http.NotFoundHandler()).ServeHTTP(rec, req)

	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"),
		"an origin the policy does not name gets no cross-origin answer")
}

func TestCORSWithNoOriginsIsAPassthrough(t *testing.T) {
	handle := CORS(config.CORS{})

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()

	handle(inner).ServeHTTP(rec, req)

	require.Equal(t, http.StatusTeapot, rec.Code,
		"an empty policy must not stand between a request and its handler")
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}
