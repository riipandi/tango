package registry

import (
	"net/http"
	"time"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/account"
	"github.com/riipandi/tango/modules/identity/apiaccess"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/ldapsync"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/webauthn"
	"github.com/riipandi/tango/pkg/crypto"

	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/federation/discovery"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/modules/federation/scimsync"
)

// Identity feature selectors, one line each in the feature list.
// Placeholders until implemented: no routes, no storage.
func withWebAuthn(deps Deps) identity.Feature  { return webauthn.New() }
func withAPIKeys(deps Deps) identity.Feature   { return apikey.New() }
func withAPIAccess(deps Deps) identity.Feature { return apiaccess.New() }
func withLDAPSync(deps Deps) identity.Feature  { return ldapsync.New() }

// newIdentityFeatures builds the mandatory user core plus the
// selected features. The guard chain: session cookie auth wraps
// RequireAdmin; the user core mounts its admin routes behind both.
// Password verifies credentials headlessly; session owns the
// sign-in/sign-out routes; account mounts self-service under the
// same guard.
func newIdentityFeatures(deps Deps, audit *auditlog.Module) (identity.APIFeature, []identity.Feature) {
	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmArgon2id)

	passwords := password.NewService(password.NewPostgresStore(deps.DB), hasher, auditAdapter(audit))
	sessions := session.NewService(
		session.NewPostgresStore(deps.DB),
		passwords,
		user.NewPostgresStore(deps.DB),
		auditAdapter(audit),
		session.WithLifetime(time.Duration(deps.Config.Auth.SessionLifetime)*time.Second),
		session.WithCookieSecure(deps.Config.App.Mode != "development"),
	)

	auth := middleware.RequireAuth(sessions, session.CookieName)
	adminAuth := func(next http.Handler) http.Handler {
		return auth(middleware.RequireAdmin(next))
	}

	core := user.NewService(
		user.NewPostgresStore(deps.DB),
		auditAdapter(audit),
		user.WithAdminGuard(adminAuth),
	)

	features := []identity.Feature{
		account.NewService(user.NewPostgresStore(deps.DB), passwords, sessions, auditAdapter(audit)),
		passwords,
		sessions,
		withWebAuthn(deps),
		withAPIKeys(deps),
		withAPIAccess(deps),
		withLDAPSync(deps),
	}
	return core, features
}

// Identity-provider feature selectors, one line each in
// federation.New. Placeholders until implemented: no routes,
// no storage. The federation module is optional: removing its
// registration (and this file's federation imports) yields a
// pure internal-identity binary.
func withOIDC(deps Deps) federation.Feature      { return oidc.New() }
func withSCIMSync(deps Deps) federation.Feature  { return scimsync.New() }
func withDiscovery(deps Deps) federation.Feature { return discovery.New() }
