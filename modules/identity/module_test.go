package identity

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/samber/do/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authv1connect "github.com/riipandi/tango/codegen/proto/go/tango/auth/v1/authv1connect"
	identityv1connect "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1/identityv1connect"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/modules/identity/jwks"
)

// TestTheAreaForwardsFeatureProcedures pins the RPC forwarding through the
// seam the registry uses: the area's Package registers the services and its
// Mount resolves them into the Deps. A provider or a resolution missed there
// leaves the feature's service nil, features() skips it without an error,
// and every procedure it holds answers "unknown procedure" — an area test
// that hand-builds Deps pins nothing about that wiring.
func TestTheAreaForwardsFeatureProcedures(t *testing.T) {
	cfg := testConfig(t)
	i := do.New(
		do.Eager(&cfg),
		do.Eager[*slog.Logger](nil),
		do.Eager[*datastore.Postgres](nil),
		// The verification feature builds over the infrastructure the
		// registry resolves; the area test pins the forwarding, not the
		// mail or queue wiring, so nils stand in for what the composition
		// root guarantees to be present.
		do.Eager[*mailer.Service](nil),
		do.Eager[*queue.Client](nil),
		// The user feature stages its pictures into the engine; nil stands
		// in for the wiring the composition root guarantees, and the picture
		// procedures refuse while the account procedures serve.
		do.Eager[*storage.Manager](nil),
	)
	Package(i)

	module, err := Mount(i)
	require.NoError(t, err)

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
	assert.True(t, claimed[identityv1connect.SignupServiceSignupProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.SignupServiceCreateSignupTokenProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.SignupServiceListSignupTokensProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.SignupServiceDeleteSignupTokenProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceListUsersProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceGetUserProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceCreateUserProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceUpdateUserProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceDeleteUserProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceUpdateProfilePictureProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.UserServiceResetProfilePictureProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.EmailVerificationServiceSendEmailProcedure],
		"the area must forward its features' procedures to the RPC router")
	assert.True(t, claimed[identityv1connect.EmailVerificationServiceVerifyEmailProcedure],
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
