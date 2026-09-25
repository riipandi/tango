package identity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authv1connect "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1/authv1connect"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/signin"
)

// TestTheAreaForwardsFeatureProcedures pins the RPC forwarding: the area
// implements kernel.RPCModule, because the composition root names the area
// alone. An area without the interface is skipped by kernel.MountRPC
// silently, and every procedure it holds answers "unknown procedure".
func TestTheAreaForwardsFeatureProcedures(t *testing.T) {
	deps := Deps{
		KeySet: jwks.NewService(testConfig(t), nil, nil),
		SignIn: signin.NewService(testConfig(t), nil, nil, nil),
	}

	var module kernel.Module = NewModule(deps)
	rpc, ok := module.(kernel.RPCModule)
	require.True(t, ok, "the area must implement kernel.RPCModule")

	router := chi.NewRouter()
	rpc.MountRPC(router)

	claimed := map[string]bool{}
	for _, route := range router.Routes() {
		claimed[route.Pattern] = true
	}
	assert.True(t, claimed[authv1connect.AuthServiceSignInProcedure],
		"the area must forward its features' procedures to the RPC router")
}

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
