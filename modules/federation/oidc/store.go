package oidc

import (
	"context"
	"time"

	"github.com/riipandi/tango/internal/datastore"
)

// Table constants owned by this module.
const (
	oidcClientsTable              = "public.oidc_clients"
	oidcClientsAllowedGroupsTable = "public.oidc_clients_allowed_user_groups"
	oidcAuthorizationCodesTable   = "public.oidc_authorization_codes"
	userAuthorizedClientsTable    = "public.user_authorized_oidc_clients"
	oauth2SessionsTable           = "public.oauth2_sessions"
	oauth2JtisTable               = "public.oauth2_jtis"
	oidcDeviceCodesTable          = "public.oidc_device_codes"
	interactionSessionsTable      = "public.interaction_sessions"

	usersTable           = "public.users"
	userGroupsTable      = "public.user_groups"
	userGroupsUsersTable = "public.user_groups_users"
	customClaimsTable    = "public.custom_claims"
)

// Store owns every OIDC/OAuth 2.0 read and write: client CRUD, the
// one-time codes, the Fosite-style session bookkeeping (kinds:
// access, refresh, authorize_code), the replay registry (oauth2_jtis),
// and the interaction bridge. Token hashes are stored, never raw.
type Store interface {
	// Clients.
	CreateClient(ctx context.Context, params ClientCreateParams) (Client, error)
	ListClients(ctx context.Context) ([]Client, error)
	GetClient(ctx context.Context, id OIDCClientID) (Client, error)
	UpdateClient(ctx context.Context, id OIDCClientID, params ClientUpdateParams) (Client, error)
	DeleteClient(ctx context.Context, id OIDCClientID) error
	SetClientGroups(ctx context.Context, id OIDCClientID, groupIDs []string) error

	// One-time authorization codes.
	InsertCode(ctx context.Context, code AuthorizationCode) error
	ConsumeCode(ctx context.Context, codeHash string) (AuthorizationCode, error)

	// Device authorizations (RFC 8628).
	InsertDeviceCode(ctx context.Context, code DeviceCode) error
	GetDeviceCode(ctx context.Context, deviceCodeHash string) (DeviceCode, error)
	GetDeviceCodeByUserCode(ctx context.Context, userCodeHash string) (DeviceCode, error)
	ApproveDeviceCode(ctx context.Context, userCodeHash, userID string) error
	DenyDeviceCode(ctx context.Context, userCodeHash string) error
	// ConsumeDeviceCode atomically flips an approved authorization to
	// consumed and returns it; any other state is ErrInvalidGrant.
	ConsumeDeviceCode(ctx context.Context, deviceCodeHash string) (DeviceCode, error)
	// UpdateDeviceCodeGrant stamps the resolved audience and granted
	// scope on approval.
	UpdateDeviceCodeGrant(ctx context.Context, userCodeHash, audience, scope string) error
	TouchDevicePoll(ctx context.Context, deviceCodeHash string, at time.Time) error
	PruneDeviceCodes(ctx context.Context, before time.Time) (int64, error)

	// OAuth 2.0 session bookkeeping + replay registry.
	PutSession(ctx context.Context, session OAuth2Session) error
	GetSession(ctx context.Context, kind, key string) (OAuth2Session, error)
	DeactivateSession(ctx context.Context, kind, key string) error
	DeactivateFamily(ctx context.Context, requestID string) error
	RecordJTI(ctx context.Context, jti string, expiresAt time.Time) error
	JTIExists(ctx context.Context, jti string) (bool, error)

	// Consent memory: scopes the user already granted a client.
	UpsertAuthorizedClient(ctx context.Context, userID, clientID string, scopes []string) error
	// HasAuthorizedClient reports whether the user ever granted the
	// client; the end-session contract requires an existing grant.
	HasAuthorizedClient(ctx context.Context, userID, clientID string) (bool, error)

	// Interaction bridge for the SPA flow.
	CreateInteraction(ctx context.Context, session InteractionSession) error
	GetInteraction(ctx context.Context, id InteractionSessionID) (InteractionSession, error)
	UpdateInteraction(ctx context.Context, id InteractionSessionID, userID *string, consentRequired *bool) error
	DeleteInteraction(ctx context.Context, id InteractionSessionID) error

	// Claim readers for token issuance.
	UserByID(ctx context.Context, userID string) (UserProfile, error)
	UserGroups(ctx context.Context, userID string) ([]string, error)
	CustomClaims(ctx context.Context, userID string) (map[string]string, error)
	UserInGroup(ctx context.Context, userID, groupID string) bool

	// Client-facing surfaces.
	AccessibleClients(ctx context.Context, userID string) ([]Client, error)
	// AuthorizedClients lists consent records; a nil userID means all users (admin view).
	AuthorizedClients(ctx context.Context, userID *string) ([]AuthorizedClient, error)
	DeleteAuthorization(ctx context.Context, userID, clientID string) error
	// RevokeClientTokens kills a user's active token family rows for one client (authorization revocation cascade).
	RevokeClientTokens(ctx context.Context, clientID, userID string) error

	// Multi-secret management (credentials JSONB + legacy column).
	AddClientSecret(ctx context.Context, clientID OIDCClientID, entry ClientSecret, rawHash string) error
	DeleteClientSecret(ctx context.Context, clientID OIDCClientID, secretID string) error

	// Client logo: blob path and image type.
	// column, cleared together.
	SetClientLogoPath(ctx context.Context, id OIDCClientID, path *string) error

	// RefreshClientMetadata rewrites the document-owned columns for a
	// CIMD client.
	RefreshClientMetadata(ctx context.Context, id OIDCClientID, params ClientUpdateParams) error
}

// PostgresStore implements Store over the shared pool. Methods live
// in the per-entity store files (store_client.go, store_authz.go,
// store_token.go, store_identity.go).
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// nullIfEmpty maps "" to nil for nullable columns.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// orDefault substitutes a fallback for empty strings (NOT NULL
// DEFAULT columns must not receive explicit NULLs).
func orDefault(value, fallback string) any {
	if value == "" {
		return fallback
	}
	return value
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}
