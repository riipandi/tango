package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/modules/identity/jwks"
)

// TestTheAreaMountsItsFeatures is the reason the area exists: the registry
// names one module, and the features inside it are reachable without the
// registry knowing them.
func TestTheAreaMountsItsFeatures(t *testing.T) {
	// A service with no configured key still mounts: the endpoint serves an
	// empty set rather than being absent.
	router := chi.NewRouter()
	NewModule(Deps{KeySet: jwks.NewService(testConfig(t), nil, nil)}).Mount(router)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, jwks.Path, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"keys"`)
}

// TestNameIsTheArea keeps the composition report readable.
func TestNameIsTheArea(t *testing.T) {
	assert.Equal(t, "identity", NewModule(Deps{}).Name())
}

// TestTheAreaMountsWithoutEveryDependency covers the area before a feature
// lands: a nil service must not panic the mount, because a deployment without
// a key pair is a valid one.
func TestTheAreaMountsWithoutEveryDependency(t *testing.T) {
	assert.NotPanics(t, func() {
		NewModule(Deps{}).Mount(chi.NewRouter())
	})
}

// testConfig returns a configuration with no key pair, which is the state a
// deployment that signs with the HMAC secret alone is in.
func testConfig(t *testing.T) config.Config {
	t.Helper()

	cfg := config.Default()
	cfg.Auth.PublicKey = ""
	cfg.Auth.PrivateKey = ""
	return cfg
}
