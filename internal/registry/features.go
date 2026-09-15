package registry

import (
	"context"
	"crypto/sha256"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/appconfig"
	"github.com/riipandi/tango/modules/appimage"
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
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/modules/identity/webauthn"
	"github.com/riipandi/tango/modules/webhook"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/web"
)

// Identity feature constructors used by the registry.
func withLDAPSync(deps Deps, exec datastore.Executor, settingsSource *appconfigRef) *ldapsync.APIFeature {
	service := ldapsync.NewService(exec, logger.Slog(deps.Logger))
	settings := func(ctx context.Context) (ldapsync.LDAPSettings, error) {
		values, ok := settingsSource.values(ctx)
		if !ok {
			return ldapsync.SettingsFromEnv(deps.Config), nil
		}
		return ldapsync.FromMergedValues(values), nil
	}
	return ldapsync.New(service, settings)
}

// appconfigRef lets identity features read settings after registration.
type appconfigRef struct {
	mu     sync.RWMutex
	module *appconfig.Module
}

// Attach sets the appconfig module.
func (r *appconfigRef) Attach(module *appconfig.Module) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.module = module
}

// values returns merged settings when appconfig is attached.
func (r *appconfigRef) values(ctx context.Context) (map[string]string, bool) {
	r.mu.RLock()
	module := r.module
	r.mu.RUnlock()
	if module == nil {
		return nil, false
	}
	values, err := module.MergedValues(ctx)
	if err != nil {
		return nil, false
	}
	return values, true
}

// withAppImageStore builds the image service and seeds bundled defaults.
func withAppImageStore(deps Deps, store storage.Store) (*appimage.Service, error) {
	defaults, err := appimage.SeedDefaults(context.Background(), store, web.ImagesDir)
	if err != nil {
		return nil, err
	}
	return appimage.NewService(store, defaults), nil
}

// defaultPictureProvider adapts appimage defaults to the user service.
func defaultPictureProvider(images *appimage.Service) user.DefaultPictureFunc {
	return func(ctx context.Context) (io.ReadCloser, int64, string, bool) {
		reader, size, mime, err := images.GetImage(ctx, appimage.ImageProfilePic)
		if err != nil {
			return nil, 0, "", false
		}
		return reader, size, mime, true
	}
}

// withAPIKeys builds the API key service and verifier.
func withAPIKeys(deps Deps, sessions *session.Service, audit *auditlog.Module) *apikey.Service {
	return apikey.NewService(
		apikey.NewPostgresStore(deps.DB),
		auditAdapter(audit),
		apikey.WithSelfAuth(sessions),
	)
}

// withWebAuthn builds the passkey feature.
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

// newWebhookModule builds the webhook service and queue processor.
func newWebhookModule(deps Deps, guard func(http.Handler) http.Handler) *webhook.Module {
	service := webhook.NewService(
		webhook.NewPostgresStore(deps.DB),
		deps.DB,
		deps.Queue,
		secretCipher(deps),
		deps.Logger,
		webhook.WithSender(webhook.NewFetcherSender(deps.Fetcher)),
	)
	service.RegisterQueue(deps.Queue)
	m := webhook.New(service)
	m.UseGuard(guard)
	return m
}

// secretCipher derives the AES-256 key used to seal module secrets.
func secretCipher(deps Deps) *crypto.Cipher {
	key := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	sealer, err := crypto.NewCipher(key[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	return sealer
}

// newVersionFeed builds the cached release lookup.
func newVersionFeed(deps Deps) *jobs.VersionFeed {
	return jobs.NewVersionFeed(deps.Fetcher, deps.Config.Public.VersionCheckURL)
}

// registerRecurringJobs registers cleanup and release-feed jobs.
func registerRecurringJobs(deps Deps, feed *jobs.VersionFeed, webhooks *webhook.Module) {
	if deps.Jobs == nil {
		return
	}
	deps.Jobs.AddJob(jobs.CleanupTokens(deps.DB, deps.Logger))
	deps.Jobs.AddJob(jobs.CleanupWebhookLogs(webhooks.Store(), deps.Logger))
	deps.Jobs.AddJob(jobs.RemindExpiringAPIKeys(deps.DB, deps.Jobs, deps.Logger))
	deps.Jobs.AddJob(jobs.VersionJob(feed, deps.Logger))
}

// newIdentityFeatures builds the user service and identity features.
func newIdentityFeatures(deps Deps, audit *auditlog.Module, recorder identity.Recorder, ldapSettingsSource *appconfigRef) (identity.APIFeature, []identity.Feature, func(http.Handler) http.Handler, *session.Service, *apiaccess.PostgresStore, *appimage.Service) {
	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt)

	// Share one blob backend across images and client logos.
	blobStore, err := storage.New(deps.Config.Storage)
	if err != nil {
		panic("registry: storage init: " + err.Error())
	}
	images, err := withAppImageStore(deps, blobStore)
	if err != nil {
		panic("registry: appimage init: " + err.Error())
	}

	passwords := password.NewService(password.NewPostgresStore(deps.DB), hasher, recorder)
	sessions := session.NewService(
		session.NewPostgresStore(deps.DB),
		passwords,
		user.NewPostgresStore(deps.DB),
		recorder,
		session.WithLifetime(time.Duration(deps.Config.Auth.SessionLifetime)*time.Second),
		session.WithCookieSecure(deps.Config.App.Mode != "development"),
	)

	auth := middleware.RequireAuth(sessions, session.CookieName)
	adminAuth := func(next http.Handler) http.Handler {
		return auth(middleware.RequireAdmin(next))
	}

	apiKeys := withAPIKeys(deps, sessions, audit)

	core := user.NewService(
		user.NewPostgresStore(deps.DB),
		recorder,
		user.WithAdminGuard(adminAuth),
		user.WithAPIKeyGuard(middleware.RequireAPIKey(apiKeys.Verify)),
		user.WithSelfAuth(sessions, session.CookieName),
		user.WithImages(blobStore, defaultPictureProvider(images)),
	)

	features := []identity.Feature{
		account.NewService(user.NewPostgresStore(deps.DB), passwords, sessions, recorder),
		passwords,
		sessions,
		usergroup.NewService(usergroup.NewPostgresStore(deps.DB), recorder, usergroup.WithAdminGuard(adminAuth)),
		customclaim.NewService(customclaim.NewPostgresStore(deps.DB), recorder, customclaim.WithAdminGuard(adminAuth)),
		withWebAuthn(deps, audit, sessions, adminAuth),
		devicelogin.New(devicelogin.NewService(
			devicelogin.NewPostgresStore(deps.DB),
			sessions,
			user.NewPostgresStore(deps.DB),
			recorder,
			devicelogin.WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		)).WithSelfAuth(sessions, session.CookieName, deps.Config.App.Mode != "development"),
		onetimeaccess.New(onetimeaccess.NewService(
			token.NewStore(deps.DB, token.PurposeOneTimeAccess),
			user.NewPostgresStore(deps.DB),
			sessions,
			recorder,
			onetimeaccess.WithMail(deps.Jobs, deps.Config.Public.BaseURL),
		)).WithAdminGuard(adminAuth).
			WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		emailverification.New(emailverification.NewService(
			token.NewStore(deps.DB, token.PurposeEmailVerification),
			emailVerificationAdapter(user.NewPostgresStore(deps.DB)),
			recorder,
			emailverification.WithMail(deps.Jobs, user.NewPostgresStore(deps.DB), deps.Config.Public.BaseURL),
		)).WithSelfAuth(sessions, session.CookieName),
		signup.New(signup.NewService(
			signup.NewPostgresStore(deps.DB),
			user.NewPostgresStore(deps.DB),
			usergroup.NewPostgresStore(deps.DB),
			sessions,
			recorder,
		)).WithAdminGuard(adminAuth).
			WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		apiaccess.NewService(apiaccess.NewPostgresStore(deps.DB), recorder, apiaccess.WithAdminGuard(adminAuth)),
		apiKeys,
	}

	ldapSync := withLDAPSync(deps, deps.DB, ldapSettingsSource)
	ldapSync.UseGuard(adminAuth)
	features = append(features, ldapSync)

	images.UseGuard(adminAuth)
	return core, features, adminAuth, sessions, apiaccess.NewPostgresStore(deps.DB), images
}

// withOIDC builds the OIDC provider feature.
func withOIDC(deps Deps, audit *auditlog.Module, keys *jwks.Service, sessions *session.Service, adminGuard func(http.Handler) http.Handler, apiAccess *apiaccess.PostgresStore, images oidc.ClientImageStore, appconfigModule *appconfig.Module) federation.Feature {
	issuer := strings.TrimRight(deps.Config.Public.BaseURL, "/")
	service := oidc.NewService(
		oidc.NewPostgresStore(deps.DB),
		jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL),
		issuer,
		session.CookieName,
		oidc.WithAudit(federationAuditAdapter(audit)),
		oidc.WithAuthenticator(sessions),
		oidc.WithAPIAccess(apiAccess),
		oidc.WithImages(images),
		oidc.WithMetadataFetcher(deps.Fetcher),
		oidc.WithCIMDAllowlist(cimdAllowlistGetter(appconfigModule)),
		oidc.WithCookieSecure(deps.Config.App.Mode != "development"),
	)
	return oidc.New(service).
		WithAdminGuard(adminGuard).
		WithSelfAuth(sessions, session.CookieName)
}

// cimdAllowlistGetter returns the configured CIMD URL allowlist.
func cimdAllowlistGetter(module *appconfig.Module) func() []string {
	return func() []string {
		values, err := module.MergedValues(context.Background())
		if err != nil {
			return nil
		}
		return appconfig.CIMDAllowlist(values)
	}
}

// federationAuditAdapter adapts auditlog for federation events.
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

// emailVerificationVerifier adapts email verification to the user store.
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

// emailVerificationAdapter returns the verifier adapter.
func emailVerificationAdapter(users user.Store) emailverification.Verifier {
	return emailVerificationVerifier{users: users}
}

// withSCIMSync builds the SCIM provisioning feature.
func withSCIMSync(deps Deps) federation.Feature {
	cipherKey := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	cipher, err := crypto.NewCipher(cipherKey[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	store := scimsync.NewPostgresStore(deps.DB, cipher)
	source := scimsync.NewIdentitySnapshotSource(deps.DB)
	service := scimsync.NewService(store, source, logger.Slog(deps.Logger))
	return scimsync.New(service)
}

// newKeyService builds the JWKS key service.
func newKeyService(deps Deps) *jwks.Service {
	cipherKey := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	cipher, err := crypto.NewCipher(cipherKey[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	return jwks.NewService(jwks.NewPostgresStore(deps.DB), cipher, jwks.RS256)
}

// withDiscovery mounts the well-known endpoints.
func withDiscovery(deps Deps, keys *jwks.Service) federation.Feature {
	provider := jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL)
	return discovery.New(provider, deps.Config.Public.BaseURL)
}
