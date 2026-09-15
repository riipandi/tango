// Package oidc is the OpenID Connect / OAuth 2.0 provider surface:
// /authorize + PAR (root router), /api/oidc/token, userinfo,
// introspect, revoke, end_session. One flat package, file-per-area;
// split into subpackages only if a coherent boundary emerges.
//
// Files: schema.go (contracts), client.go (relying-party clients),
// authorize.go (authorize + interaction), token.go (codes, tokens,
// refresh rotation), userinfo.go, store.go (persistence).
//
// Authentication happens in identity features (passkeys, sessions);
// this package consumes identity contracts via consumer-side adapters.
package oidc

import (
	"context"
	"errors"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/kernel"
)

// Typed IDs for the OIDC/OAuth 2.0 tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name (lowercase letters and
// underscores only, per the TypeID spec). Only IDs that appear in
// URLs or cross-module references carry a TypeID — token/code rows
// are addressed by their SHA-256 key and keep their DB uuidv7 IDs.
type (
	oidcClientPrefix struct{}

	OIDCClientID = typeid.TypeID[oidcClientPrefix]

	interactionSessionPrefix struct{}

	InteractionSessionID = typeid.TypeID[interactionSessionPrefix]
)

func (oidcClientPrefix) Prefix() string         { return "oidc_client" }
func (interactionSessionPrefix) Prefix() string { return "interaction_session" }

// NewID mints a client ID; its string form is the clients.id TEXT
// primary key and the JWT client_id claim.
func NewID() OIDCClientID { return typeid.Must(typeid.New[OIDCClientID]()) }

// NewInteractionID mints an interaction session ID.
func NewInteractionID() InteractionSessionID {
	return typeid.Must(typeid.New[InteractionSessionID]())
}

// InteractionIDFromString parses an interaction ID from its string
// form.
func InteractionIDFromString(raw string) (InteractionSessionID, error) {
	return typeid.Parse[InteractionSessionID](raw)
}

// Endpoint paths, token lifetimes, and protocol constants.
const (
	AuthorizePath = "/authorize"
	TokenPath     = "/api/oidc/token"
	UserInfoPath  = "/api/oidc/userinfo"
	// InteractionPath is the SPA page resolving an interaction
	// session (sign-in / consent); the API lives under /api/oidc/interaction.
	InteractionPath = "/interaction"

	// ClientsAPIPath mounts the admin client CRUD under /api.
	ClientsAPIPath = "/api/oidc/clients"

	// AuthorizationCodeTTL bounds the one-time code lifetime.
	AuthorizationCodeTTL = 2 * time.Minute
	// AccessTokenTTL and RefreshTokenTTL are defaults; per-client
	// durations override both.
	AccessTokenTTL  = 60 * time.Minute
	RefreshTokenTTL = 30 * 24 * time.Hour
	// InteractionSessionTTL bounds the sign-in/consent bridge.
	InteractionSessionTTL = 15 * time.Minute
)

// Scopes understood by this provider.
const (
	ScopeOpenID  = "openid"
	ScopeEmail   = "email"
	ScopeProfile = "profile"
	ScopeGroups  = "groups"
)

// APIAccessProvider is implemented by the identity apiaccess
// feature; it lets the provider resolve RFC 8707 resources to
// audiences and per-client permission keys without importing the
// identity tree.
type APIAccessProvider interface {
	// AllowedScopesForAudience returns the permission keys the
	// client may request for the API behind the resource, whether
	// such an API exists, and whether the client may reach it at
	// all. A client with access but no permission gets an empty
	// scope list with hasAccess true.
	AllowedScopesForAudience(ctx context.Context, clientID, resource string, subjectType string) (scopes []string, apiExists bool, hasAccess bool, err error)
}

// Feature is the wireable oidc unit backed by the provider service.
// The admin guard (auth → RequireAdmin) applies to client
// management at mount time (handler.go); the self authenticator
// gates the users/me surfaces. Both fail closed when absent.
type Feature struct {
	service    *Service
	adminGuard kernel.Guard
	selfAuth   kernel.Authenticator
	cookieName string
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name implements federation.Feature.
func (Feature) Name() string { return "oidc" }

// WithAdminGuard registers the admin guard for client management.
func (f Feature) WithAdminGuard(guard kernel.Guard) Feature {
	f.adminGuard = guard
	return f
}

// WithSelfAuth registers the session resolver for users/me surfaces.
func (f Feature) WithSelfAuth(auth kernel.Authenticator, cookieName string) Feature {
	f.selfAuth, f.cookieName = auth, cookieName
	return f
}

// Errors surfaced to relying parties (RFC 6749 §5.2) and handlers.
var (
	ErrNotFound = errors.New("oidc: not found")
	// ErrInvalidClient maps to the invalid_client token error.
	ErrInvalidClient = errors.New("oidc: client authentication failed")
	// ErrInvalidGrant maps to the invalid_grant token error: bad or
	// expired code, wrong verifier, replayed refresh token.
	ErrInvalidGrant = errors.New("oidc: grant is invalid")
	// ErrInvalidRequest covers malformed authorize/token parameters.
	ErrInvalidRequest = errors.New("oidc: request is invalid")
	// ErrAccessDenied covers group-restricted clients whose user is
	// not a member of any allowed group.
	ErrAccessDenied = errors.New("oidc: access denied")
	// ErrUnauthorizedClient rejects clients using grants they may
	// not use.
	ErrUnauthorizedClient = errors.New("oidc: client is not authorized for this grant")
)
