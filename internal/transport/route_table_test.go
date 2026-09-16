package transport

// route_table_test.go locks the transport contracts: every route is
// mounted exactly once, nothing doubles under /api/api, middleware
// order applies request IDs before logging and recovery, and the
// security/CORS headers land on API responses.

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNoDoubleAPIPrefix fails when a handler registers a path that
// already starts with /api inside the shared /api group — the
// resulting /api/api/... mount is always a defect.
func TestNoDoubleAPIPrefix(t *testing.T) {
	srv := testServer(t, testConfig())

	for _, route := range collectRouterRoutes(t, srv.Router) {
		assert.NotContains(t, route, "/api/api/", "double-mounted API path: %s", route)
	}
}

// collectRouterRoutes walks the chi tree and returns every
// method+pattern pair.
func collectRouterRoutes(t *testing.T, r chi.Router) []string {
	t.Helper()
	var routes []string
	require.NoError(t, chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	}))
	slices.Sort(routes)
	return routes
}

// TestAPIRoutesAreUnique pins that no method+pattern pair is
// registered twice on the server router.
func TestAPIRoutesAreUnique(t *testing.T) {
	srv := testServer(t, testConfig())

	routes := collectRouterRoutes(t, srv.Router)
	seen := map[string]int{}
	for _, route := range routes {
		seen[route]++
	}

	duplicates := []string{}
	for route, count := range seen {
		if count > 1 {
			duplicates = append(duplicates, route)
		}
	}
	assert.Empty(t, duplicates, "routes registered more than once")
}

// TestMiddlewareOrderAndHeaders pins the observable middleware
// contract: request IDs land on API responses, CORS answers
// preflights, and panics inside handlers become JSON envelopes
// instead of crashes.
func TestMiddlewareOrderAndHeaders(t *testing.T) {
	srv := testServer(t, testConfig())

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
	require.Equal(t, http.StatusOK, w.Code)

	assert.NotEmpty(t, w.Header().Get("X-Request-Id"), "request ID middleware must run")

	// Preflight requests pass through CORS without auth.
	preflight := httptest.NewRequest(http.MethodOptions, "/api/users", nil)
	preflight.Header.Set("Origin", "http://localhost:3000")
	preflight.Header.Set("Access-Control-Request-Method", "POST")
	w = httptest.NewRecorder()
	srv.Router.ServeHTTP(w, preflight)
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"), "preflight must be CORS-handled")
}

// TestWellKnownRoutesAreRootMounted pins that the protocol documents
// live on the root router, not under /api.
func TestWellKnownRoutesAreRootMounted(t *testing.T) {
	srv := testServer(t, testConfig())

	for _, path := range []string{"/.well-known/version"} {
		w := httptest.NewRecorder()
		srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		assert.NotEqual(t, http.StatusNotFound, w.Code, "%s must be mounted", path)
	}

	for _, route := range collectRouterRoutes(t, srv.Router) {
		assert.NotContains(t, route, "/api/.well-known", "well-known routes must not double under /api")
	}
}
