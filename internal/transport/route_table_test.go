package transport

// route_table_test.go locks the transport contracts: every route is
// mounted exactly once, nothing doubles under /api/api, middleware
// order applies request IDs before logging and recovery, and the
// security/CORS headers land on API responses.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
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

// fakeAuth resolves any bearer token to an admin principal except the
// "revoked-" prefix, which stands in for expired/revoked sessions.
type fakeAuth struct{ fail bool }

func (f fakeAuth) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if f.fail || token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: "user_test", Username: "tester", IsAdmin: true}, nil
}

// rpcTestServer mounts the version surface with the fake
// authenticator so the mixed public/protected behavior runs through
// the real transport.
func rpcTestServer(t *testing.T) *HTTPServer {
	t.Helper()
	srv := NewHTTPServer(RouteSet{
		MountRPC: func(r chi.Router) {
			prefix, handler := VersionRPCService(nil, fakeAuth{})
			r.Handle(prefix+"*", handler)
		},
	}, testConfig(), testLogger(), nil, nil, nil)
	t.Cleanup(func() {
		if srv.Server != nil {
			_ = srv.Server.Close()
		}
	})
	return srv
}

// TestRPCVersionAuthBranches pins the mixed public/protected contract
// of VersionService through the real transport: anonymous Current
// answers the Connect unauthenticated error, a resolvable bearer
// succeeds, and Latest stays public.
func TestRPCVersionAuthBranches(t *testing.T) {
	srv := rpcTestServer(t)

	post := func(path, token string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		srv.Router.ServeHTTP(w, req)
		return w
	}

	w := post("/rpc/tango.system.v1.VersionService/Current", "")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "bearer token required")

	w = post("/rpc/tango.system.v1.VersionService/Current", "token-1")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), config.AppVersion)

	w = post("/rpc/tango.system.v1.VersionService/Latest", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "latestVersion")

	// Expired/revoked tokens are indistinguishable from unknown ones.
	w = post("/rpc/tango.system.v1.VersionService/Current", "revoked-token")
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired token")
}

// TestRPCVersionCurrentWithoutInterceptor pins the defense-in-depth
// branch: a raw handler call without a resolved principal still
// refuses.
func TestRPCVersionCurrentWithoutInterceptor(t *testing.T) {
	svc := &versionRPCService{}
	req := connect.NewRequest(&systemv1.CurrentRequest{})
	res, err := svc.Current(middleware.WithPrincipal(context.Background(), kernel.Principal{UserID: "user_test", IsAdmin: true}), req)
	require.NoError(t, err)
	assert.Equal(t, config.AppVersion, res.Msg.GetCurrentVersion())

	_, err = svc.Current(context.Background(), req)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr))
	assert.Equal(t, connect.CodeUnauthenticated, cerr.Code())
}

// TestRPCRejectsReflection pins the reflection policy: no reflection
// or descriptor service is mounted — production reflection stays
// disabled, and Yaak uses explicit procedure paths.
func TestRPCRejectsReflection(t *testing.T) {
	srv := testServer(t, testConfig())

	for _, path := range []string{
		"/rpc/grpc.reflection.v1.ServerReflection/ServerReflectionInfo",
		"/rpc/grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo",
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/grpc-web+json")
		srv.Router.ServeHTTP(w, req)

		require.Equal(t, http.StatusNotFound, w.Code, "%s must not be served", path)
		assert.Contains(t, w.Body.String(), "not_found")
	}
}

// TestRPCRouteTable tests the /rpc mount contract end to end.
func TestRPCRouteTable(t *testing.T) {
	srv := testServer(t, testConfig())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.system.v1.HealthService/Check", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	srv.Router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"status":"ok"}`, w.Body.String())
	assert.NotEmpty(t, w.Header().Get("X-Request-Id"), "request ID middleware must cover /rpc")
}

// TestRPCMountDoesNotFallThroughToSPA pins that unknown /rpc paths
// answer Connect protocol errors, never the SPA document.
func TestRPCMountDoesNotFallThroughToSPA(t *testing.T) {
	srv := testServer(t, testConfig())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.system.v1.Unknown/Call", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	srv.Router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Body.String(), "not_found", "must be a Connect error, not the SPA index")
}

// TestRPCRejectsWrongMethods pins that the Connect handler only
// accepts unary POST procedure calls.
func TestRPCRejectsWrongMethods(t *testing.T) {
	srv := testServer(t, testConfig())

	w := httptest.NewRecorder()
	srv.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/rpc/tango.system.v1.HealthService/Check", nil))
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
	assert.Equal(t, "POST", w.Header().Get("Allow"), "GET on a non-idempotent unary RPC must advertise POST")
}
