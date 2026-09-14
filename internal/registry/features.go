package registry

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/config"
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
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/modules/identity/webauthn"
	"github.com/riipandi/tango/modules/webhook"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/web"
)

// Identity feature selectors, one line each in the feature list.
// Placeholders until implemented: no routes, no storage.
func withLDAPSync(deps Deps, exec datastore.Executor, settingsSource *appconfigRef) *ldapsync.APIFeature {
	service := ldapsync.NewService(exec, logger.Slog(deps.Logger))
	settings := func(ctx context.Context) (ldapsync.LDAPSettings, error) {
		values, ok := settingsSource.values(ctx)
		if !ok {
			return envLDAPSettings(deps.Config), nil
		}
		return mapLDAPSettings(values), nil
	}
	feature := ldapsync.New(service, settings)
	feature.WithGuard(nil) // guard wired by the caller below
	return feature
}

// mapLDAPSettings reads the merged appconfig values (env defaults
// already folded) into the sync settings. Bool parsing is lenient:
// anything but "true" is off, mirroring the env behavior.
func mapLDAPSettings(values map[string]string) ldapsync.LDAPSettings {
	return ldapsync.LDAPSettings{
		Enabled:           values["ldap_enabled"] == "true",
		URL:               values["ldap_url"],
		BindDN:            values["ldap_bind_dn"],
		BindPassword:      values["ldap_bind_password"],
		Base:              values["ldap_base"],
		UserFilter:        values["ldap_user_search_filter"],
		GroupFilter:       values["ldap_user_group_search_filter"],
		SkipCertVerify:    values["ldap_skip_cert_verify"] == "true",
		AttrUserUniqueID:  values["ldap_attribute_user_unique_identifier"],
		AttrUserUsername:  values["ldap_attribute_user_username"],
		AttrUserEmail:     values["ldap_attribute_user_email"],
		AttrUserFirstName: values["ldap_attribute_user_first_name"],
		AttrUserLastName:  values["ldap_attribute_user_last_name"],
		AttrUserDisplay:   values["ldap_attribute_user_display_name"],
		AttrGroupUniqueID: values["ldap_attribute_group_unique_identifier"],
		AttrGroupName:     values["ldap_attribute_group_name"],
		AttrGroupMember:   values["ldap_attribute_group_member"],
		AdminGroupName:    values["ldap_admin_group_name"],
		SoftDeleteUsers:   values["ldap_soft_delete_users"] == "true",
	}
}

// MailerSettingsFromValues builds the relay config from merged
// appconfig values, falling back to the env config per field. The
// mailer settings source resolves through this per send.
func MailerSettingsFromValues(values map[string]string, fallback config.MailerConfig) config.MailerConfig {
	out := fallback
	if v := values["smtp_from_email"]; v != "" {
		out.FromEmail = v
	}
	if v := values["smtp_from_name"]; v != "" {
		out.FromName = v
	}
	if v := values["smtp_host"]; v != "" {
		out.SMTPHost = v
	}
	if v := values["smtp_port"]; v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			out.SMTPPort = port
		}
	}
	if v, ok := values["smtp_username"]; ok {
		out.SMTPUsername = v
	}
	if v, ok := values["smtp_password"]; ok {
		out.SMTPPassword = v
	}
	if v := values["smtp_secure"]; v != "" {
		out.SMTPSecure = v == "true"
	}
	return out
}

// appConfigEnvDefaults maps the koanf LDAP/Mailer sections onto the
// appconfig keys as the env layer of the defaults fold.
func appConfigEnvDefaults(cfg *config.Config) map[string]string {
	return map[string]string{
		"smtp_from_email":                        cfg.Mailer.FromEmail,
		"smtp_from_name":                         cfg.Mailer.FromName,
		"smtp_host":                              cfg.Mailer.SMTPHost,
		"smtp_port":                              strconv.Itoa(cfg.Mailer.SMTPPort),
		"smtp_username":                          cfg.Mailer.SMTPUsername,
		"smtp_password":                          cfg.Mailer.SMTPPassword,
		"smtp_secure":                            strconv.FormatBool(cfg.Mailer.SMTPSecure),
		"ldap_enabled":                           strconv.FormatBool(cfg.LDAP.Enabled),
		"ldap_url":                               cfg.LDAP.URL,
		"ldap_bind_dn":                           cfg.LDAP.BindDN,
		"ldap_bind_password":                     cfg.LDAP.BindPassword,
		"ldap_base":                              cfg.LDAP.Base,
		"ldap_user_search_filter":                cfg.LDAP.UserFilter,
		"ldap_user_group_search_filter":          cfg.LDAP.GroupFilter,
		"ldap_skip_cert_verify":                  strconv.FormatBool(cfg.LDAP.SkipCertVerify),
		"ldap_attribute_user_unique_identifier":  cfg.LDAP.AttrUserUniqueID,
		"ldap_attribute_user_username":           cfg.LDAP.AttrUserUsername,
		"ldap_attribute_user_email":              cfg.LDAP.AttrUserEmail,
		"ldap_attribute_user_first_name":         cfg.LDAP.AttrUserFirstName,
		"ldap_attribute_user_last_name":          cfg.LDAP.AttrUserLastName,
		"ldap_attribute_user_display_name":       cfg.LDAP.AttrUserDisplay,
		"ldap_attribute_group_unique_identifier": cfg.LDAP.AttrGroupUniqueID,
		"ldap_attribute_group_name":              cfg.LDAP.AttrGroupName,
		"ldap_attribute_group_member":            cfg.LDAP.AttrGroupMember,
		"ldap_admin_group_name":                  cfg.LDAP.AdminGroupName,
		"ldap_soft_delete_users":                 strconv.FormatBool(cfg.LDAP.SoftDeleteUsers),
	}
}

// envLDAPSettings maps env config onto the sync settings until
// appconfig lands; admin-editable keys take over in a later pass.
func envLDAPSettings(cfg *config.Config) ldapsync.LDAPSettings {
	return ldapsync.LDAPSettings{
		Enabled:           cfg.LDAP.Enabled,
		URL:               cfg.LDAP.URL,
		BindDN:            cfg.LDAP.BindDN,
		BindPassword:      cfg.LDAP.BindPassword,
		Base:              cfg.LDAP.Base,
		UserFilter:        cfg.LDAP.UserFilter,
		GroupFilter:       cfg.LDAP.GroupFilter,
		SkipCertVerify:    cfg.LDAP.SkipCertVerify,
		AttrUserUniqueID:  cfg.LDAP.AttrUserUniqueID,
		AttrUserUsername:  cfg.LDAP.AttrUserUsername,
		AttrUserEmail:     cfg.LDAP.AttrUserEmail,
		AttrUserFirstName: cfg.LDAP.AttrUserFirstName,
		AttrUserLastName:  cfg.LDAP.AttrUserLastName,
		AttrUserDisplay:   cfg.LDAP.AttrUserDisplay,
		AttrGroupUniqueID: cfg.LDAP.AttrGroupUniqueID,
		AttrGroupName:     cfg.LDAP.AttrGroupName,
		AttrGroupMember:   cfg.LDAP.AttrGroupMember,
		AdminGroupName:    cfg.LDAP.AdminGroupName,
		SoftDeleteUsers:   cfg.LDAP.SoftDeleteUsers,
	}
}

// withLDAPSync callsite in newIdentityFeatures receives the ref; the
// appconfig module attaches itself after registration below.
type appconfigRef struct {
	mu     sync.RWMutex
	module *appconfig.Module
}

// Attach wires the module once it exists.
func (r *appconfigRef) Attach(module *appconfig.Module) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.module = module
}

// values reads the merged settings; ok is false until the module
// attached (then callers fall back to the env-only settings).
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

// withAppImageStore builds the branding-image feature over the given
// blob backend and seeds bundled defaults at startup.
func withAppImageStore(deps Deps, store storage.Store) (*appimage.Service, error) {
	defaults, err := appimage.SeedDefaults(context.Background(), store, web.ImagesDir)
	if err != nil {
		return nil, err
	}
	return appimage.NewService(store, defaults), nil
}

// defaultPictureProvider adapts the appimage default to the user
// module's fallback hook.
func defaultPictureProvider(images *appimage.Service) user.DefaultPictureFunc {
	return func(ctx context.Context) (io.ReadCloser, int64, string, bool) {
		reader, size, mime, err := images.GetImage(ctx, appimage.ImageProfilePic)
		if err != nil {
			return nil, 0, "", false
		}
		return reader, size, mime, true
	}
}

// withAPIKeys builds the machine-credential feature: self-scoped
// key CRUD plus the X-API-KEY verifier for the transport middleware.
func withAPIKeys(deps Deps, sessions *session.Service, audit *auditlog.Module) *apikey.Service {
	return apikey.NewService(
		apikey.NewPostgresStore(deps.DB),
		auditAdapter(audit),
		apikey.WithSelfAuth(sessions),
	)
}

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

// newWebhookModule builds the outbound webhook surface: endpoints,
// signed deliveries through the shared fetcher, and the queue
// processor that performs them. The signing secret is sealed under a
// digest of auth.secret_key, the same derivation the JWKS halves use.
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
	return webhook.New(service).WithAdminGuard(guard)
}

// secretCipher derives the AES-256 key sealing module secrets at rest
// from auth.secret_key: SHA-256 always yields 32 bytes, so the
// constructor cannot fail on otherwise valid configuration.
func secretCipher(deps Deps) *crypto.Cipher {
	key := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	sealer, err := crypto.NewCipher(key[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	return sealer
}

// newVersionFeed builds the cached latest-release lookup over the
// shared outbound client.
func newVersionFeed(deps Deps) *jobs.VersionFeed {
	return jobs.NewVersionFeed(deps.Fetcher, deps.Config.Public.VersionCheckURL)
}

// registerRecurringJobs wires the maintenance cadence: expired tokens,
// delivery history, and the release feed. Webhook log pruning runs
// through the webhook store.
func registerRecurringJobs(deps Deps, feed *jobs.VersionFeed, webhooks *webhook.Module) {
	if deps.Jobs == nil {
		return
	}
	deps.Jobs.AddJob(jobs.CleanupTokens(deps.DB, deps.Logger))
	deps.Jobs.AddJob(jobs.CleanupWebhookLogs(webhooks.Store(), deps.Logger))
	deps.Jobs.AddJob(jobs.RemindExpiringAPIKeys(deps.DB, deps.Jobs, deps.Logger))
	deps.Jobs.AddJob(jobs.VersionJob(feed, deps.Logger))
}

// newIdentityFeatures builds the mandatory user core plus the
// selected features. The guard chain: session cookie auth wraps
// RequireAdmin; the user core mounts its admin routes behind both.
// Password verifies credentials headlessly; session owns the
// sign-in/sign-out routes; account mounts self-service under the
// same guard. Returns the sessions feature so the federation
// surface can resolve session cookies (optional-auth /authorize).
func newIdentityFeatures(deps Deps, audit *auditlog.Module, recorder identity.Recorder, ldapSettingsSource *appconfigRef) (identity.APIFeature, []identity.Feature, func(http.Handler) http.Handler, *session.Service, *apiaccess.PostgresStore, *appimage.Service) {
	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt)

	// One blob backend shared by app images, profile pictures, and
	// client logos.
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
			onetimeaccess.NewPostgresStore(deps.DB),
			user.NewPostgresStore(deps.DB),
			sessions,
			recorder,
			onetimeaccess.WithMail(deps.Jobs, deps.Config.Public.BaseURL),
		)).WithAdminGuard(adminAuth).
			WithCookie(session.CookieName, deps.Config.App.Mode != "development"),
		emailverification.New(emailverification.NewService(
			emailverification.NewPostgresStore(deps.DB),
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
		withLDAPSync(deps, deps.DB, ldapSettingsSource),
	}
	images.WithGuard(adminAuth)
	return core, features, adminAuth, sessions, apiaccess.NewPostgresStore(deps.DB), images
}

// withOIDC builds the provider feature: claim readers from the
// store, token signing via the Phase 3 key service, session-cookie
// resolution via the identity session feature (optional-auth
// /authorize), the audit adapter, and the admin guard for client
// management.
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

// cimdAllowlistGetter reads the operator-managed CIMD URL allowlist
// from the app config (JSON array; default deny when unset).
func cimdAllowlistGetter(module *appconfig.Module) func() []string {
	return func() []string {
		values, err := module.MergedValues(context.Background())
		if err != nil {
			return nil
		}
		var allowlist []string
		_ = json.Unmarshal([]byte(values["cimd_url_allowlist"]), &allowlist)
		return allowlist
	}
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

// withSCIMSync builds the outbound SCIM provisioning feature: tokens
// encrypted under the same cipher derivation as the JWKS halves.
func withSCIMSync(deps Deps) federation.Feature {
	cipherKey := sha256.Sum256([]byte(deps.Config.Auth.SecretKey))
	cipher, err := crypto.NewCipher(cipherKey[:])
	if err != nil {
		panic("registry: cipher key derivation is always 32 bytes: " + err.Error())
	}
	store := scimsync.NewPostgresStore(deps.DB, cipher)
	source := scimSnapshotSource{db: deps.DB}
	service := scimsync.NewService(store, source, logger.Slog(deps.Logger))
	return scimsync.New(service)
}

// scimSnapshotSource adapts the identity stores to the SCIM snapshot
// contract, scoped by the client's group allowlist.
type scimSnapshotSource struct {
	db datastore.Store
}

func (s scimSnapshotSource) UsersForClient(ctx context.Context, clientID string) ([]scimsync.ScimUserRow, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("DISTINCT u.id", "u.username", "u.display_name", "u.first_name", "u.last_name", "u.email", "u.disabled")
	sb.From("public.users AS u")
	sb.Join("public.user_groups_users AS ugu", "ugu.user_id = u.id")
	sb.Join("public.oidc_clients_allowed_user_groups AS ag", "ag.user_group_id = ugu.user_group_id")
	sb.Where(sb.E("ag.oidc_client_id", clientID))
	return scimUserRows(ctx, s.db, sb)
}

func (s scimSnapshotSource) GroupsForClient(ctx context.Context, clientID string) ([]scimsync.ScimGroupRow, error) {
	// Groups the client may see, with their member user IDs.
	groups := sqlbuilder.PostgreSQL.NewSelectBuilder()
	groups.Select("g.id", "g.name")
	groups.From("public.user_groups AS g")
	groups.Join("public.oidc_clients_allowed_user_groups AS ag", "ag.user_group_id = g.id")
	groups.Where(groups.E("ag.oidc_client_id", clientID))
	gQuery, gArgs := groups.Build()

	rows, err := s.db.Query(ctx, gQuery, gArgs...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot groups: %w", err)
	}
	defer rows.Close()

	var out []scimsync.ScimGroupRow
	ids := make([]string, 0, 8)
	for rows.Next() {
		var row scimsync.ScimGroupRow
		var idText string
		if scanErr := rows.Scan(&idText, &row.Name); scanErr != nil {
			return nil, fmt.Errorf("scimsync: scan group: %w", scanErr)
		}
		row.ID = idText
		ids = append(ids, idText)
		out = append(out, row)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, rowsErr
	}
	if len(out) == 0 {
		return out, nil
	}

	// Members of those groups.
	members := sqlbuilder.PostgreSQL.NewSelectBuilder()
	members.Select("user_group_id", "user_id")
	members.From("public.user_groups_users")
	members.Where(members.In("user_group_id", toAny(ids)...))
	mQuery, mArgs := members.Build()
	mRows, err := s.db.Query(ctx, mQuery, mArgs...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot members: %w", err)
	}
	defer mRows.Close()

	byGroup := map[string][]string{}
	for mRows.Next() {
		var groupID, userID string
		if scanErr := mRows.Scan(&groupID, &userID); scanErr != nil {
			return nil, fmt.Errorf("scimsync: scan member: %w", scanErr)
		}
		byGroup[groupID] = append(byGroup[groupID], userID)
	}
	for i := range out {
		out[i].Members = byGroup[out[i].ID]
	}
	return out, mRows.Err()
}

func scimUserRows(ctx context.Context, db datastore.Executor, sb *sqlbuilder.SelectBuilder) ([]scimsync.ScimUserRow, error) {
	query, args := sb.Build()
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("scimsync: snapshot users: %w", err)
	}
	defer rows.Close()

	var out []scimsync.ScimUserRow
	for rows.Next() {
		var row scimsync.ScimUserRow
		var disabled bool
		if err := rows.Scan(&row.ID, &row.Username, &row.DisplayName, &row.FirstName, &row.LastName, &row.Email, &disabled); err != nil {
			return nil, fmt.Errorf("scimsync: scan user: %w", err)
		}
		row.Active = !disabled
		out = append(out, row)
	}
	return out, rows.Err()
}

func toAny[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

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
