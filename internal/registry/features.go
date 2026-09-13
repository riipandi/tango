package registry

import (
	"context"
	"crypto/sha256"
	"net/http"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/federation/discovery"
	"github.com/riipandi/tango/modules/federation/jwks"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/modules/federation/scimsync"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/account"
	"github.com/riipandi/tango/modules/identity/apiaccess"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/customclaim"
	"github.com/riipandi/tango/modules/identity/devicelogin"
	"github.com/riipandi/tango/modules/identity/emailverification"
	"github.com/riipandi/tango/modules/identity/ldapsync"
	"github.com/riipandi/tango/modules/identity/onetimeaccess"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/signup"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/modules/identity/webauthn"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// Identity feature selectors, one line each in the feature list.
// Placeholders until implemented: no routes, no storage.
func withAPIKeys(deps Deps) identity.Feature   { return apikey.New() }
func withAPIAccess(deps Deps) identity.Feature { return apiaccess.New() }
func withLDAPSync(deps Deps) identity.Feature  { return ldapsync.New() }

// withWebAuthn builds the passkey feature over the shared key
// material: session issue via the sessions feature, app URL as the
// relying-party identity, admin guard + self auth at mount time.
// The error path panics only on an invalid app URL shape (boot
// misconfiguration).
func withWebAuthn(deps Deps, audit *auditlog.Module, sessions *session.Service, adminAuth func(http.Handler) http.Handler) identity.Feature {
	appURL := strings.TrimRight(deps.Config.Public.BaseURL, "/")
	service, err := webauthn.NewService(
		webauthn.NewPostgresStore(deps.DB),
		user.NewPostgresStore(deps.DB),
		func(ctx context.Context, userID user.UserID) (string, error) {
			return sessions.IssueForUser(ctx, userID, "passkey", session.Meta{})
		},
		appURL,
		auditAdapter(audit),
		webauthn.WithCookieSecure(deps.Config.App.Mode != "development"),
		webauthn.WithCookieName(session.CookieName),
	)
	if err != nil {
		panic("registry: webauthn init: " + err.Error())
	}
	return webauthn.New(service).WithAdminGuard(adminAuth).WithSelfAuth(sessions, session.CookieName)
}

// newIdentityFeatures builds the mandatory user core plus the
// selected features. The guard chain: session cookie auth wraps
// RequireAdmin; the user core mounts its admin routes behind both.
// Password verifies credentials headlessly; session owns the
// sign-in/sign-out routes; account mounts self-service under the
// same guard. Returns the sessions feature so the federation
// surface can resolve session cookies (optional-auth /authorize).
func newIdentityFeatures(deps Deps, audit *auditlog.Module) (identity.APIFeature, []identity.Feature, func(http.Handler) http.Handler, *session.Service) {
	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt)

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
		user.WithSelfAuth(sessions, session.CookieName),
	)

	features := []identity.Feature{
		account.NewService(user.NewPostgresStore(deps.DB), passwords, sessions, auditAdapter(audit)),
		passwords,
		sessions,
		usergroup.NewService(usergroup.NewPostgresStore(deps.DB), auditAdapter(audit), usergroup.WithAdminGuard(adminAuth)),
		customclaim.NewService(customclaim.NewPostgresStore(deps.DB), auditAdapter(audit), customclaim.WithAdminGuard(adminAuth)),
		withWebAuthn(deps, audit, sessions, adminAuth),
		devicelogin.New(devicelogin.NewService(
			devicelogin.NewPostgresStore(deps.DB),
			sessions,
			user.NewPostgresStore(deps.DB),
			auditAdapter(audit),
			devicelogin.WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		)).WithSelfAuth(sessions, session.CookieName, deps.Config.App.Mode != "development"),
		onetimeaccess.New(onetimeaccess.NewService(
			onetimeaccess.NewPostgresStore(deps.DB),
			user.NewPostgresStore(deps.DB),
			sessions,
			auditAdapter(audit),
		)).WithAdminGuard(adminAuth).
			WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		emailverification.New(emailverification.NewService(
			emailverification.NewPostgresStore(deps.DB),
			emailVerificationAdapter(user.NewPostgresStore(deps.DB)),
			auditAdapter(audit),
		)).WithSelfAuth(sessions, session.CookieName),
		signup.New(signup.NewService(
			signup.NewPostgresStore(deps.DB),
			user.NewPostgresStore(deps.DB),
			usergroup.NewPostgresStore(deps.DB),
			sessions,
			auditAdapter(audit),
		)).WithAdminGuard(adminAuth).
			WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		withAPIKeys(deps),
		withAPIAccess(deps),
		withLDAPSync(deps),
	}
	return core, features, adminAuth, sessions
}

// withOIDC builds the provider feature: claim readers from the
// store, token signing via the Phase 3 key service, session-cookie
// resolution via the identity session feature (optional-auth
// /authorize), the audit adapter, and the admin guard for client
// management.
func withOIDC(deps Deps, audit *auditlog.Module, keys *jwks.Service, sessions *session.Service, adminGuard func(http.Handler) http.Handler) federation.Feature {
	issuer := strings.TrimRight(deps.Config.Public.BaseURL, "/")
	service := oidc.NewService(
		oidc.NewPostgresStore(deps.DB),
		jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL),
		issuer,
		session.CookieName,
		oidc.WithAudit(federationAuditAdapter(audit)),
		oidc.WithAuthenticator(sessions),
		oidc.WithCookieSecure(deps.Config.App.Mode != "development"),
	)
	return oidc.New(service).
		WithAdminGuard(adminGuard).
		WithSelfAuth(sessions, session.CookieName)
}

// federationAuditAdapter adapts auditlog for federation events:
// oidc events carry a plain action plus a payload map.
func federationAuditAdapter(audit *auditlog.Module) func(context.Context, string, map[string]any) {
	return func(ctx context.Context, event string, params map[string]any) {
		entry := auditlog.Entry{
			Event:   event,
			Trigger: auditlog.TriggerUser,
			Status:  auditlog.StatusSuccess,
			Payload: params,
		}
		_ = audit.Record(ctx, &entry)
	}
}

// emailVerificationVerifier adapts the emailverification Verifier
// contract to the user store (typed IDs at the boundary).
type emailVerificationVerifier struct {
	users user.Store
}

func (a emailVerificationVerifier) MarkEmailVerified(ctx context.Context, userID string) error {
	id, err := identity.ParseID[user.UserID](userID)
	if err != nil {
		return err
	}
	return a.users.MarkEmailVerified(ctx, id)
}

// emailVerificationAdapter returns the verifier adapter value.
func emailVerificationAdapter(users user.Store) emailverification.Verifier {
	return emailVerificationVerifier{users: users}
}

// withSCIMSync is a placeholder until its phase lands: no routes, no storage.
func withSCIMSync(deps Deps) federation.Feature { return scimsync.New() }

// newKeyService builds the JWKS key service (private halves
// encrypted at rest under a digest of auth.secret_key). It is a
// startable feature: registry startup guarantees a signing key
// exists before the server binds.
func newKeyService(deps Deps) *jwks.Service {
	cipherKey := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	cipher, err := crypto.NewCipher(cipherKey[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	return jwks.NewService(jwks.NewPostgresStore(deps.DB), cipher, jwks.RS256)
}

// withDiscovery mounts the well-known endpoints in front of the
// TTL-cached key provider shared with token issuance.
func withDiscovery(deps Deps, keys *jwks.Service) federation.Feature {
	provider := jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL)
	return discovery.New(provider, deps.Config.Public.BaseURL)
}
