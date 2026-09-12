// Package identity owns internal authn/authz: user accounts plus the
// selectable features wired at the composition root. The identity
// provider surface for other systems lives in the optional
// modules/federation package.
package identity

import (
	"context"
	"time"

	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/kernel"
)

// NewID generates a fresh UUIDv7-backed ID of the given type. It
// panics only on an invalid prefix, which cannot happen for the
// prefixes declared here.
func NewID[T typeid.Subtype, PT typeid.SubtypePtr[T]]() T {
	return typeid.Must(typeid.New[T, PT]())
}

// ParseID decodes and validates a typed ID string, rejecting
// mismatches between the wire prefix and the expected type.
func ParseID[T typeid.Subtype, PT typeid.SubtypePtr[T]](s string) (T, error) {
	return typeid.Parse[T, PT](s)
}

// Typed IDs for the identity tables: a UUIDv7 suffix plus a
// snake_case prefix matching the singular table name, so values are
// self-describing in logs, URLs, and API payloads while staying
// K-sortable and index-friendly in Postgres.
type (
	userPrefix struct{}

	// UserID identifies a users row.
	UserID = typeid.TypeID[userPrefix]

	userGroupPrefix struct{}

	// UserGroupID identifies a user_groups row.
	UserGroupID = typeid.TypeID[userGroupPrefix]

	userPhonePrefix struct{}

	// UserPhoneID identifies a user_phones row.
	UserPhoneID = typeid.TypeID[userPhonePrefix]

	sessionPrefix struct{}

	// SessionID identifies a sessions row (stored as the full TypeID string).
	SessionID = typeid.TypeID[sessionPrefix]

	authTokenPrefix struct{}

	// AuthTokenID identifies an auth_tokens row.
	AuthTokenID = typeid.TypeID[authTokenPrefix]

	signupTokenPrefix struct{}

	// SignupTokenID identifies a signup_tokens row.
	SignupTokenID = typeid.TypeID[signupTokenPrefix]

	refreshTokenPrefix struct{}

	// RefreshTokenID identifies a refresh_tokens row.
	RefreshTokenID = typeid.TypeID[refreshTokenPrefix]

	invitationPrefix struct{}

	// InvitationID identifies an invitations row.
	InvitationID = typeid.TypeID[invitationPrefix]

	mfaKeyPrefix struct{}

	// MFAKeyID identifies an mfa_keys row.
	MFAKeyID = typeid.TypeID[mfaKeyPrefix]

	webauthnCredentialPrefix struct{}

	// WebauthnCredentialID identifies a webauthn_credentials row.
	WebauthnCredentialID = typeid.TypeID[webauthnCredentialPrefix]

	webauthnSessionPrefix struct{}

	// WebauthnSessionID identifies a webauthn_sessions row.
	WebauthnSessionID = typeid.TypeID[webauthnSessionPrefix]

	apiKeyPrefix struct{}

	// APIKeyID identifies an api_keys row.
	APIKeyID = typeid.TypeID[apiKeyPrefix]

	oauthConnectionPrefix struct{}

	// OAuthConnectionID identifies an oauth_connections row.
	OAuthConnectionID = typeid.TypeID[oauthConnectionPrefix]
)

func (userPrefix) Prefix() string               { return "user" }
func (userGroupPrefix) Prefix() string          { return "user_group" }
func (userPhonePrefix) Prefix() string          { return "user_phone" }
func (sessionPrefix) Prefix() string            { return "session" }
func (authTokenPrefix) Prefix() string          { return "auth_token" }
func (signupTokenPrefix) Prefix() string        { return "signup_token" }
func (refreshTokenPrefix) Prefix() string       { return "refresh_token" }
func (invitationPrefix) Prefix() string         { return "invitation" }
func (mfaKeyPrefix) Prefix() string             { return "mfa_key" }
func (webauthnCredentialPrefix) Prefix() string { return "webauthn_credential" }
func (webauthnSessionPrefix) Prefix() string    { return "webauthn_session" }
func (apiKeyPrefix) Prefix() string             { return "api_key" }
func (oauthConnectionPrefix) Prefix() string    { return "oauth_connection" }

// User is the core entity, shared by all features. The optional
// federation module consumes it via adapters. Nullable columns use
// pointers; empty string is never stored for those.
type User struct {
	ID        UserID  `json:"id"`
	Username  string  `json:"username"`
	Email     string  `json:"email"`
	FirstName *string `json:"first_name,omitzero"`
	LastName  *string `json:"last_name,omitzero"`
	AvatarURL *string `json:"avatar_url,omitzero"`
	Locale    *string `json:"locale,omitzero"`

	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	Disabled    bool   `json:"disabled"`

	EmailVerifiedAt *time.Time `json:"email_verified_at,omitzero"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitzero"`
	LastLoginAt     *time.Time `json:"last_login_at,omitzero"`
}

// Recorder receives audit events; the composition root adapts any
// sink (e.g. auditlog) so features never import it directly. The
// context flows from the originating request.
type Recorder func(ctx context.Context, event AuditEvent)

// AuditEvent is what features emit to a Recorder.
type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// Feature is one selectable unit chosen at the composition root. A
// feature left out of New does not exist: no routes, no storage, no
// lifecycle.
type Feature interface {
	Name() string
}

// APIFeature mounts endpoints inside the shared /api group.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
}

// StartableFeature holds resources with a lifecycle: started after
// the module core, stopped before it.
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature mounts routes on the root router, outside /api.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
