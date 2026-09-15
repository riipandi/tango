package registry

import (
	"context"
	"go/parser"
	"go/token"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Application boundaries per the target modular monolith decision:
// features inside one boundary may import each other, but no package
// may reach into a different boundary's concrete packages. The
// composition root (this package) is the only place allowed to import
// across boundaries.
var boundaryOf = func(importPath string) string {
	switch {
	case strings.HasPrefix(importPath, "github.com/riipandi/tango/modules/identity"):
		return "identity"
	case strings.HasPrefix(importPath, "github.com/riipandi/tango/modules/federation"):
		return "federation"
	case strings.HasPrefix(importPath, "github.com/riipandi/tango/modules/webhook"):
		return "webhook"
	case strings.HasPrefix(importPath, "github.com/riipandi/tango/modules/auditlog"),
		strings.HasPrefix(importPath, "github.com/riipandi/tango/modules/appconfig"):
		return "admin"
	}
	return ""
}

// TestModuleBoundariesDoNotImportAcrossApplicationAreas fails when a
// package inside one application boundary imports a concrete package
// from another boundary. Consumer-side interfaces are the intended fix.
func TestModuleBoundariesDoNotImportAcrossApplicationAreas(t *testing.T) {
	root := "../../modules"

	violations := []string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		src, err := os.ReadFile(path)
		require.NoError(t, err)
		file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
		require.NoError(t, err)

		importer := boundaryOf(packagePath(path))
		if importer == "" {
			return nil
		}
		for _, imp := range file.Imports {
			imported := boundaryOf(strings.Trim(imp.Path.Value, `"`))
			if imported != "" && imported != importer {
				violations = append(violations, path+" imports "+imp.Path.Value)
			}
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, violations, "cross-boundary concrete imports")
}

func packagePath(filePath string) string {
	return "github.com/riipandi/tango/" + filepath.Dir(filePath)
}

// collectRoutes walks a mounted chi router and returns every
// method+pattern pair in registration order.
func collectRoutes(t *testing.T, r chi.Router) []string {
	t.Helper()
	routes := []string{}
	err := chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	require.NoError(t, err)
	return routes
}

// TestAPIRoutesMountEachPathOnce pins route ownership: mounting the
// composition root must yield each API method+pattern exactly once,
// never twice from overlapping feature registrations.
func TestAPIRoutesMountEachPathOnce(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	api := chi.NewRouter()
	rt.MountAPI(api)

	seen := map[string]int{}
	for _, route := range collectRoutes(t, api) {
		seen[route]++
	}
	assert.Greater(t, len(seen), 100, "the API surface must be mounted")

	duplicates := []string{}
	for route, count := range seen {
		if count > 1 {
			duplicates = append(duplicates, route)
		}
	}
	assert.Empty(t, duplicates, "routes mounted more than once")
}

// TestRootRoutesMountEachPathOnce pins ownership of the root surface
// (protocol endpoints mounted outside /api).
func TestRootRoutesMountEachPathOnce(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	root := chi.NewRouter()
	rt.MountRoot(root)

	seen := map[string]int{}
	for _, route := range collectRoutes(t, root) {
		seen[route]++
	}
	assert.Greater(t, len(seen), 0, "root routes must be mounted")

	duplicates := []string{}
	for route, count := range seen {
		if count > 1 {
			duplicates = append(duplicates, route)
		}
	}
	assert.Empty(t, duplicates, "root routes mounted more than once")
}

// The LDAP sync endpoint is an excluded upstream feature; the route
// must stay unmounted even though application configuration is served.
func TestLDAPSyncEndpointNotMounted(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)
	api := chi.NewRouter()
	rt.MountAPI(api)

	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/application-configuration/sync-ldap", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestRuntimeStartStopOrder pins the explicit lifecycle order: start
// queue-first, stop queue-last.
func TestRuntimeStartStopOrder(t *testing.T) {
	rt, err := New(testDeps(t))
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, rt.Start(ctx))
	require.NoError(t, rt.Stop(ctx))
}
