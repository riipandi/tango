package oidc

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// Service implements the provider flows using injected identity and
// session providers.
type Service struct {
	store         Store
	keys          jwtutils.KeyProvider
	authenticator kernel.Authenticator
	apiAccess     APIAccessProvider
	issuer        string
	cookieName    string
	cookieSecure  bool
	audit         AuditLogger
	images        ClientImageStore

	metadataFetcher DocumentFetcher
	cimdAllowlist   func() []string

	// Protocol lifetimes; zero values fall back to the package
	// defaults via each lifetime() accessor.
	accessTokenTTL       time.Duration
	refreshTokenTTL      time.Duration
	authorizationCodeTTL time.Duration
	interactionTTL       time.Duration
	deviceCodeTTL        time.Duration
	parTTL               time.Duration

	// scimBinding resolves one client's SCIM provider projection for
	// the GetScimProvider procedure; the scimsync feature owns the
	// data, so the registry adapts its store onto this function.
	scimBinding func(ctx context.Context, clientID string) (*ScimBinding, error)

	// access resolves the internal bearer token for the consent
	// surface's per-procedure guard.
	access kernel.AccessAuthenticator
}

// AuditLogger receives audit events; the registry adapts auditlog.
// Func type keeps the provider module-free.
type AuditLogger func(ctx context.Context, event string, params map[string]any)

// NewService builds the provider service.
func NewService(store Store, keys jwtutils.KeyProvider, issuer, cookieName string, opts ...Option) *Service {
	s := &Service{store: store, keys: keys, issuer: issuer, cookieName: cookieName}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Option customizes the service.
type Option func(*Service)

// lifetime resolves a configured duration, falling back to the
// package default when unset.
func lifetime(configured, fallback time.Duration) time.Duration {
	if configured > 0 {
		return configured
	}
	return fallback
}

// AccessTokenTTL is the provider default access-token lifetime.
func (s *Service) AccessTokenTTL() time.Duration {
	return lifetime(s.accessTokenTTL, DefaultAccessTokenTTL)
}

// RefreshTokenTTL is the provider default refresh-token lifetime.
func (s *Service) RefreshTokenTTL() time.Duration {
	return lifetime(s.refreshTokenTTL, DefaultRefreshTokenTTL)
}

// AuthorizationCodeTTL bounds the one-time code lifetime.
func (s *Service) AuthorizationCodeTTL() time.Duration {
	return lifetime(s.authorizationCodeTTL, DefaultAuthorizationCodeTTL)
}

// InteractionTTL bounds the sign-in/consent bridge.
func (s *Service) InteractionTTL() time.Duration {
	return lifetime(s.interactionTTL, DefaultInteractionSessionTTL)
}

// DeviceCodeTTL bounds the device authorization window.
func (s *Service) DeviceCodeTTL() time.Duration {
	return lifetime(s.deviceCodeTTL, DefaultDeviceCodeTTL)
}

// PARTTL bounds a pushed authorization request_uri lifetime.
func (s *Service) PARTTL() time.Duration {
	return lifetime(s.parTTL, DefaultPARTTL)
}

// WithAudit attaches the audit adapter.
func WithAudit(l AuditLogger) Option { return func(s *Service) { s.audit = l } }

// WithAPIAccess attaches the resource-API resolver used to enforce
// RFC 8707 resource audiences and permission scopes.
func WithAPIAccess(p APIAccessProvider) Option { return func(s *Service) { s.apiAccess = p } }

// WithScimBinding attaches the per-client SCIM provider lookup used
// by the GetScimProvider procedure.
func WithScimBinding(fn func(ctx context.Context, clientID string) (*ScimBinding, error)) Option {
	return func(s *Service) { s.scimBinding = fn }
}

// WithAccessAuthenticator attaches the internal bearer resolver used
// by the consent surface's per-procedure guard.
func WithAccessAuthenticator(auth kernel.AccessAuthenticator) Option {
	return func(s *Service) { s.access = auth }
}

// WithAuthenticator injects the session cookie resolver used by the
// optional-auth /authorize flow.
func WithAuthenticator(auth kernel.Authenticator) Option {
	return func(s *Service) { s.authenticator = auth }
}

// WithImages wires the blob backend for the client-logo surface.
func WithImages(images ClientImageStore) Option {
	return func(s *Service) { s.images = images }
}

// WithMetadataFetcher wires the outbound document downloader.
func WithMetadataFetcher(f DocumentFetcher) Option {
	return func(s *Service) { s.metadataFetcher = f }
}

// WithCIMDAllowlist wires the operator allowlist getter (default
// deny when nil).
func WithCIMDAllowlist(get func() []string) Option {
	return func(s *Service) { s.cimdAllowlist = get }
}

// WithCookieSecure marks the end-session cookie Secure (off in
// development, mirroring the session module).
// Lifetimes groups the relying-party protocol lifetimes. A zero
// field keeps the package default, so a caller sets only what it
// configures.
type Lifetimes struct {
	AccessToken       time.Duration
	RefreshToken      time.Duration
	AuthorizationCode time.Duration
	Interaction       time.Duration
	DeviceCode        time.Duration
	PAR               time.Duration
}

// WithLifetimes overrides the protocol lifetimes.
func WithLifetimes(l Lifetimes) Option {
	return func(s *Service) {
		s.accessTokenTTL = l.AccessToken
		s.refreshTokenTTL = l.RefreshToken
		s.authorizationCodeTTL = l.AuthorizationCode
		s.interactionTTL = l.Interaction
		s.deviceCodeTTL = l.DeviceCode
		s.parTTL = l.PAR
	}
}

func WithCookieSecure(secure bool) Option {
	return func(s *Service) { s.cookieSecure = secure }
}

// record emits an audit event when an adapter is attached.
func (s *Service) record(ctx context.Context, event string, params map[string]any) {
	if s.audit != nil {
		s.audit(ctx, event, params)
	}
}

// UserClaims is the assembled identity payload for tokens.
type UserClaims struct {
	Subject           string
	SessionID         string
	Email             string
	EmailVerified     bool
	Name              string
	PreferredUsername string
	Groups            []string
	Custom            map[string]any
}

// claimsFor assembles user claims: profile columns, group names,
// and custom claims (user-scoped plus group-scoped), filtered by
// the granted scope set. Subject is normalized to the UUID column
// form — the stable `sub` every relying party sees.
func (s *Service) claimsFor(ctx context.Context, userID, scope, sid string, authTime time.Time) (UserClaims, error) {
	claims := UserClaims{Subject: datastore.UserUUID(userID), SessionID: sid, Custom: map[string]any{}}

	if scopeHas(scope, ScopeProfile) || scopeHas(scope, ScopeEmail) {
		u, err := s.store.UserByID(ctx, userID)
		if err != nil {
			return claims, err
		}
		claims.Email = u.Email
		claims.EmailVerified = u.EmailVerifiedAt != nil
		claims.Name = u.DisplayName
		claims.PreferredUsername = u.Username
	}

	if scopeHas(scope, ScopeGroups) {
		groups, err := s.store.UserGroups(ctx, userID)
		if err != nil {
			return claims, err
		}
		claims.Groups = groups
	}

	if scopeHas(scope, ScopeProfile) || scopeHas(scope, ScopeEmail) || scopeHas(scope, ScopeGroups) {
		custom, err := s.store.CustomClaims(ctx, userID)
		if err != nil {
			return claims, err
		}
		for key, value := range custom {
			claims.Custom[key] = value
		}
	}
	return claims, nil
}

// scopeHas reports whether the space-delimited scope string grants
// the named scope.
func scopeHas(scope, name string) bool {
	return scopeList(scope)[name]
}

// scopeList splits a scope string into a set.
func scopeList(scope string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Fields(scope) {
		out[part] = true
	}
	return out
}

// sha256Hex hashes a token for at-rest storage.
func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// pkceS256 computes the RFC 7636 S256 challenge for a verifier.
func pkceS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// MatchesCallback reports whether uri matches a registered callback
// pattern. Patterns match exactly, or by prefix when they carry a
// trailing `*` (the callback-url-wildcards contract).
func (c Client) MatchesCallback(uri string) bool {
	for _, pattern := range c.CallbackURLs {
		if pattern == uri {
			return true
		}
		if after, ok := strings.CutSuffix(pattern, "*"); ok && strings.HasPrefix(uri, after) {
			return true
		}
	}
	return false
}
