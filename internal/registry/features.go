package registry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/admin/apiaccess"
	"github.com/riipandi/tango/modules/admin/apikey"
	"github.com/riipandi/tango/modules/admin/appconfig"
	"github.com/riipandi/tango/modules/admin/auditlog"
	"github.com/riipandi/tango/modules/admin/customclaim"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/federation/discovery"
	"github.com/riipandi/tango/modules/federation/jwks"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/modules/federation/scimsync"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/account"
	"github.com/riipandi/tango/modules/identity/devicelogin"
	"github.com/riipandi/tango/modules/identity/emailverification"
	"github.com/riipandi/tango/modules/identity/onetimeaccess"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/recovery"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/signup"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/totp"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/modules/identity/webauthn"
	"github.com/riipandi/tango/modules/webhook"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/web"
)

// defaultPictureProvider adapts bundled defaults to the user service.
func defaultPictureProvider(images *storage.BundledImages) user.DefaultPictureFunc {
	return func(ctx context.Context) (io.ReadCloser, int64, string, bool) {
		return images.Open(ctx, storage.DefaultProfilePicture)
	}
}

// withAPIKeys builds the API key service and verifier.
func withAPIKeys(deps Deps, sessions *session.Service, audit *auditlog.Module) *apikey.Service {
	return apikey.NewService(
		apikey.NewPostgresStore(deps.DB),
		auditAdapter(audit),
	)
}

// withWebAuthn builds the passkey feature.
func withWebAuthn(deps Deps, audit *auditlog.Module, sessions *session.Service) (identity.APIFeature, *webauthn.Service) {
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
	return webauthn.New(service), service
}

// newWebhookModule builds the webhook service and queue processor.
func newWebhookModule(deps Deps, queueClient *queue.Client) *webhook.Module {
	service := webhook.NewService(
		webhook.NewPostgresStore(deps.DB),
		deps.DB,
		queueClient,
		secretCipher(deps),
		deps.Logger,
		webhook.WithSender(webhook.NewFetcherSender(deps.Fetcher)),
	)
	service.RegisterQueue(queueClient)
	return webhook.New(service)
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
func registerRecurringJobs(deps Deps, reg *jobs.Registry, feed *jobs.VersionFeed, webhooks *webhook.Module) {
	reg.AddJob(jobs.CleanupTokens(deps.DB, deps.Logger))
	reg.AddJob(jobs.CleanupWebhookDeliveries(webhooks.Store(), deps.Logger))
	reg.AddJob(jobs.RemindExpiringAPIKeys(deps.DB, reg, deps.Logger))
	reg.AddJob(jobs.VersionJob(feed, deps.Logger))
}

// newIdentityFeatures builds the identity module: sessions first, the
// audit module second (its guards need sessions), then the guarded
// features. It also returns the session service, route groups, and
// API-access store shared with the federation surface.
func newIdentityFeatures(deps Deps, jobsReg *jobs.Registry, recorder identity.Recorder, keys *jwks.Service) (*identity.Module, identity.RouteGroups, *session.Service, *auditlog.Module, *apiaccess.PostgresStore, *apikey.Service, storage.Store, error) {
	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt)

	// Share one blob backend across images and client logos.
	blobStore, err := storage.New(deps.Config.Storage)
	if err != nil {
		return nil, identity.RouteGroups{}, nil, nil, nil, nil, nil, fmt.Errorf("registry: storage init: %w", err)
	}
	bundled, err := storage.SeedBundledImages(context.Background(), blobStore, web.ImagesDir)
	if err != nil {
		return nil, identity.RouteGroups{}, nil, nil, nil, nil, nil, fmt.Errorf("registry: bundled images init: %w", err)
	}

	passwords := password.NewService(password.NewPostgresStore(deps.DB), hasher, recorder)

	// The TOTP feature binds sessions after construction: the session
	// service needs the MFA port, and verification needs the session
	// issuer.
	totpService := totp.NewService(
		totp.NewPostgresStore(deps.DB),
		user.NewPostgresStore(deps.DB),
		nil,
		passwords,
		secretCipher(deps),
		// The issuer mirrors the app_name catalog default; a live
		// rename of the instance does not rewrite enrolled links.
		"tango",
		recorder,
	)
	sessions := session.NewService(
		session.NewPostgresStore(deps.DB),
		passwords,
		user.NewPostgresStore(deps.DB),
		recorder,
		session.WithLifetime(time.Duration(deps.Config.Auth.SessionLifetime)*time.Second),
		session.WithCookieSecure(deps.Config.App.Mode != "development"),
		session.WithMFAPort(totpService),
		session.WithAccessTokens(session.NewAccessTokenSigner(
			jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL),
		)),
	)
	totpService.BindSessions(sessions)

	auth := middleware.RequireAuth(sessions, session.CookieName)
	adminAuth := func(next http.Handler) http.Handler {
		return auth(middleware.RequireAdmin(next))
	}

	audit := auditlog.New(
		auditlog.NewPostgresStore(deps.DB),
		auditlog.WithAdminGuard(adminAuth),
		auditlog.WithSelfAuth(sessions, session.CookieName),
	)

	apiKeys := withAPIKeys(deps, sessions, audit)
	groups := identity.RouteGroups{
		Admin:  adminAuth,
		Self:   auth,
		APIKey: middleware.RPCAPIKeyAuth(apiKeys.Verify),
	}

	core := user.NewService(
		user.NewPostgresStore(deps.DB),
		recorder,
		user.WithImages(blobStore, defaultPictureProvider(bundled)),
	)

	groupService := usergroup.NewService(
		usergroup.NewPostgresStore(deps.DB),
		recorder,
		usergroup.WithUserStore(user.NewPostgresStore(deps.DB)),
	)

	passkeys, webauthnService := withWebAuthn(deps, audit, sessions)

	// The user-core Connect surface binds group memberships and
	// passkey management through the neutral ports.
	core.BindRPCPorts(
		usergroup.NewUserRPCPort(groupService),
		webauthn.NewUserRPCPort(webauthnService),
	)

	module := identity.New(
		core,
		account.NewService(user.NewPostgresStore(deps.DB), passwords, sessions, recorder),
		sessions,
		groupService,
		customclaim.NewService(customclaim.NewPostgresStore(deps.DB), recorder),
		passkeys,
		devicelogin.New(devicelogin.NewService(
			devicelogin.NewPostgresStore(deps.DB),
			sessions,
			user.NewPostgresStore(deps.DB),
			recorder,
			devicelogin.WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		)),
		onetimeaccess.New(onetimeaccess.NewService(
			token.NewStore(deps.DB, token.PurposeOneTimeAccess),
			user.NewPostgresStore(deps.DB),
			sessions,
			recorder,
			onetimeaccess.WithMail(jobsReg, deps.Config.Public.BaseURL),
		)).WithCookie(session.CookieName, deps.Config.App.Mode != "development").WithAccessAuthenticator(sessions),
		emailverification.New(emailverification.NewService(
			token.NewStore(deps.DB, token.PurposeEmailVerification),
			emailVerificationAdapter(user.NewPostgresStore(deps.DB)),
			recorder,
			emailverification.WithMail(jobsReg, user.NewPostgresStore(deps.DB), deps.Config.Public.BaseURL),
		)).WithAccessAuthenticator(sessions),
		signup.New(signup.NewService(
			signup.NewPostgresStore(deps.DB),
			user.NewPostgresStore(deps.DB),
			usergroup.NewPostgresStore(deps.DB),
			sessions,
			recorder,
		)).WithCookie(session.CookieName, deps.Config.App.Mode != "development").WithAccessAuthenticator(sessions),
		apiaccess.NewService(apiaccess.NewPostgresStore(deps.DB), recorder),
		apiKeys,
		recovery.NewFeature(recovery.New(
			token.NewStore(deps.DB, token.PurposePasswordReset),
			user.NewPostgresStore(deps.DB),
			password.NewPostgresStore(deps.DB),
			sessions,
			hasher,
			recorder,
			recovery.WithMail(jobsReg, deps.Config.Public.BaseURL),
		)).WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		totp.NewFeature(totpService).WithCookie(deps.Config.App.Mode != "development").WithAccessAuthenticator(sessions),
	)

	return module, groups, sessions, audit, apiaccess.NewPostgresStore(deps.DB), apiKeys, blobStore, nil
}

// withOIDC builds the OIDC provider feature.
func withOIDC(deps Deps, audit *auditlog.Module, keys *jwks.Service, sessions *session.Service, apiAccess *apiaccess.PostgresStore, images oidc.ClientImageStore, appconfigModule *appconfig.Module, scimBinding func(context.Context, string) (*oidc.ScimBinding, error)) federation.ProviderFeature {
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
		oidc.WithScimBinding(scimBinding),
		oidc.WithAccessAuthenticator(sessions),
		oidc.WithCookieSecure(deps.Config.App.Mode != "development"),
	)
	return oidc.New(service)
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

// withSCIMSync builds the SCIM provisioning feature; the store rides
// along for the OIDC client surface's per-client binding lookup.
func withSCIMSync(deps Deps) (federation.RPCServiceProvider, *scimsync.PostgresStore) {
	cipherKey := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	cipher, err := crypto.NewCipher(cipherKey[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	store := scimsync.NewPostgresStore(deps.DB, cipher)
	source := scimsync.NewIdentitySnapshotSource(deps.DB)
	service := scimsync.NewService(store, source, newSCIMPoster(), logger.Slog(deps.Logger))
	return scimsync.New(service), store
}

// scimBindingLookup adapts the scimsync store onto the oidc
// GetScimProvider port; store sentinels map onto Connect codes.
func scimBindingLookup(store *scimsync.PostgresStore) func(context.Context, string) (*oidc.ScimBinding, error) {
	return func(ctx context.Context, clientID string) (*oidc.ScimBinding, error) {
		p, err := store.GetByClient(ctx, clientID)
		if err != nil {
			switch {
			case errors.Is(err, scimsync.ErrNotFound):
				return nil, rpcerr.NotFound("scim service provider not found")
			case errors.Is(err, scimsync.ErrUnknownClient):
				return nil, rpcerr.InvalidArgument("unknown oidc client")
			default:
				return nil, rpcerr.Internal("internal error")
			}
		}
		return &oidc.ScimBinding{
			ID:           p.ID.String(),
			Endpoint:     p.Endpoint,
			OIDCClientID: p.OIDCClientID,
			LastSyncedAt: p.LastSyncedAt,
			CreatedAt:    p.CreatedAt,
		}, nil
	}
}

// scimPoster executes outbound SCIM requests over net/http; the
// module stays transport-free behind the scimPoster port.
type scimPoster struct{ client *http.Client }

func newSCIMPoster() *scimPoster {
	return &scimPoster{client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *scimPoster) Do(ctx context.Context, req scimsync.ScimRequest) (scimsync.ScimResponse, error) {
	var body io.Reader
	if len(req.Body) > 0 {
		body = bytes.NewReader(req.Body)
	}
	out, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		return scimsync.ScimResponse{}, err
	}
	for key, value := range req.Headers {
		out.Header.Set(key, value)
	}
	resp, err := p.client.Do(out)
	if err != nil {
		return scimsync.ScimResponse{}, err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return scimsync.ScimResponse{}, err
	}
	header := map[string]string{}
	for key := range resp.Header {
		header[key] = resp.Header.Get(key)
	}
	return scimsync.ScimResponse{StatusCode: resp.StatusCode, Header: header, Body: data}, nil
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
func withDiscovery(deps Deps, keys *jwks.Service) federation.RootRoutableFeature {
	provider := jwtutils.NewCachedKeyProvider(keys, jwks.CacheTTL)
	return discovery.New(provider, deps.Config.Public.BaseURL)
}
