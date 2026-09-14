package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

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

	// OAuth 2.0 session bookkeeping + replay registry.
	PutSession(ctx context.Context, session OAuth2Session) error
	GetSession(ctx context.Context, kind, key string) (OAuth2Session, error)
	DeactivateSession(ctx context.Context, kind, key string) error
	DeactivateFamily(ctx context.Context, requestID string) error
	RecordJTI(ctx context.Context, jti string, expiresAt time.Time) error
	JTIExists(ctx context.Context, jti string) (bool, error)

	// Consent memory: scopes the user already granted a client.
	UpsertAuthorizedClient(ctx context.Context, userID, clientID string, scopes []string) error

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

	// Client-facing surfaces (upstream /users/me/clients et al).
	AccessibleClients(ctx context.Context, userID string) ([]Client, error)
	// AuthorizedClients lists consent records; a nil userID means all users (admin view).
	AuthorizedClients(ctx context.Context, userID *string) ([]AuthorizedClient, error)
	DeleteAuthorization(ctx context.Context, userID, clientID string) error
	// RevokeClientTokens kills a user's active token family rows for one client (authorization revocation cascade).
	RevokeClientTokens(ctx context.Context, clientID, userID string) error

	// Multi-secret management (credentials JSONB + legacy column).
	AddClientSecret(ctx context.Context, clientID OIDCClientID, entry ClientSecret, rawHash string) error
	DeleteClientSecret(ctx context.Context, clientID OIDCClientID, secretID string) error

	// Client logo (phase 9C): blob path + upstream-compat image_type
	// column, cleared together.
	SetClientLogoPath(ctx context.Context, id OIDCClientID, path *string) error
}

// Client is a relying party. Secrets never round-trip: the store
// keeps SHA-256 hashes in the credentials JSONB (plus the legacy
// single-secret column) and only the create-secret call sees raw.
type Client struct {
	ID                          OIDCClientID
	Name                        string
	Description                 string
	SecretHash                  *string
	Secrets                     []ClientSecret
	CallbackURLs                []string
	LogoutCallbackURLs          []string
	LaunchURL                   string
	ImageType                   *string
	DarkImageType               *string
	LogoPath                    *string
	ClientType                  string
	IsPublic                    bool
	PKCEEnabled                 bool
	PKCESupported               bool
	RequiresReauthentication    bool
	SkipConsent                 bool
	IsGroupRestricted           bool
	AccessTokenDurationMinutes  int64
	RefreshTokenDurationMinutes int64
	CreatedByID                 *string
	CreatedAt                   time.Time
	AllowedGroupIDs             []string
}

// ClientSecret is one credentials row (metadata; the hash lives in
// the JSONB, never serialized to clients).
type ClientSecret struct {
	ID         string
	SecretHash string
	CreatedAt  time.Time
	ExpiresAt  *time.Time
	IsActive   bool
}

// SecretEntryView is the API shape of a stored secret: metadata
// only, plus an optional clear prefix.
type SecretEntryView struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitzero"`
	IsActive  bool       `json:"is_active"`
}

// LegacySecretID names the synthetic entry for secrets that predate
// the credentials list.
const LegacySecretID = "legacy"

// ClientCreateParams carries admin-supplied client fields.
type ClientCreateParams struct {
	Name                        string
	Description                 string
	CallbackURLs                []string
	LogoutCallbackURLs          []string
	LaunchURL                   string
	IsPublic                    bool
	PKCEEnabled                 bool
	PKCESupported               bool
	SkipConsent                 bool
	IsGroupRestricted           bool
	AccessTokenDurationMinutes  int64
	RefreshTokenDurationMinutes int64
	CreatedByID                 string
	SecretHash                  string
}

// ClientUpdateParams patches a client; nil fields keep values.
type ClientUpdateParams struct {
	Name                        *string
	Description                 *string
	CallbackURLs                []string // nil keeps; non-nil replaces
	LogoutCallbackURLs          []string
	LaunchURL                   *string
	IsPublic                    *bool
	PKCEEnabled                 *bool
	PKCESupported               *bool
	SkipConsent                 *bool
	IsGroupRestricted           *bool
	AccessTokenDurationMinutes  *int64
	RefreshTokenDurationMinutes *int64
	SecretHash                  *string
}

// AuthorizationCode is a one-time code row; Code holds the SHA-256
// of the value sent to the relying party.
type AuthorizationCode struct {
	CodeHash      string
	Scope         string
	Nonce         string
	CodeChallenge string
	MethodSHA256  bool
	AuthMethod    string
	UserID        string
	ClientID      string
	ExpiresAt     time.Time
}

// OAuth2Session is one oauth2_sessions row (Fosite-style); Key is
// the token's jti (or code hash), RequestID ties a token family
// together for reuse revocation.
type OAuth2Session struct {
	Kind        string
	Key         string
	RequestID   string
	Active      bool
	RequestData map[string]any
	ClientID    string
	ExpiresAt   *time.Time
	CreatedAt   *time.Time
}

// Session kinds stored in oauth2_sessions.
const (
	KindAuthorizeCode = "authorize_code"
	KindAccessToken   = "access_token"
	KindRefresh       = "refresh_token"
)

// AuthorizedClient is a consent record: user × client + granted
// scopes + last use.
type AuthorizedClient struct {
	UserID     string    `json:"user_id"`
	ClientID   string    `json:"client_id"`
	Scopes     []string  `json:"scopes"`
	LastUsedAt time.Time `json:"last_used_at"`
}

// InteractionSession bridges /authorize to the SPA sign-in/consent
// flow. Parameters preserves the original authorize query for the
// resume.
type InteractionSession struct {
	ID                     InteractionSessionID
	ClientID               string
	UserID                 *string
	Scopes                 []string
	Parameters             map[string]any
	ConsentRequired        bool
	AuthenticationRequired bool
	RequestedAt            time.Time
}

// UserProfile is the claim source for tokens.
type UserProfile struct {
	ID              string
	Username        string
	Email           string
	DisplayName     string
	EmailVerifiedAt *time.Time
	Disabled        bool
}

// PostgresStore implements Store over the shared pool.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// clientColumns is the SELECT list; keep in sync with scanClient.
var clientColumns = []string{
	"c.id", "c.name", "c.description", "c.secret", "c.credentials", "c.callback_urls", "c.logout_callback_urls",
	"c.launch_url", "c.is_public", "c.pkce_enabled", "c.pkce_supported",
	"c.requires_reauthentication", "c.skip_consent", "c.is_group_restricted",
	"c.access_token_duration_minutes", "c.refresh_token_duration_minutes",
	"c.created_by_id", "c.created_at", "c.image_type", "c.dark_image_type", "c.client_type", "c.logo_path",
}

func (s *PostgresStore) clientSelect(id string) *sqlbuilder.SelectBuilder {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(clientColumns...)
	sb.From(oidcClientsTable + " c")
	sb.Where(sb.E("c.id", id))
	return sb
}

// CreateClient inserts a client; the caller supplies the secret
// hash (may be empty for public clients).
func (s *PostgresStore) CreateClient(ctx context.Context, params ClientCreateParams) (Client, error) {
	id := NewID()
	callbacks, _ := json.Marshal(params.CallbackURLs)
	logoutCallbacks, _ := json.Marshal(params.LogoutCallbackURLs)

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oidcClientsTable)
	ib.Cols(
		"id", "name", "description", "secret", "callback_urls", "logout_callback_urls", "launch_url",
		"is_public", "pkce_enabled", "pkce_supported", "skip_consent", "is_group_restricted",
		"access_token_duration_minutes", "refresh_token_duration_minutes", "created_by_id",
	)
	ib.Values(
		id.String(), params.Name, params.Description, nullIfEmpty(params.SecretHash), callbacks, logoutCallbacks, params.LaunchURL,
		params.IsPublic, params.PKCEEnabled, params.PKCESupported, params.SkipConsent, params.IsGroupRestricted,
		params.AccessTokenDurationMinutes, params.RefreshTokenDurationMinutes, nullIfEmpty(params.CreatedByID),
	)

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return Client{}, fmt.Errorf("oidc store: create client: %w", err)
	}

	created, err := s.GetClient(ctx, id)
	if err != nil {
		return Client{}, err
	}
	if len(params.CallbackURLs) == 0 {
		created.CallbackURLs = []string{}
	}
	if len(params.LogoutCallbackURLs) == 0 {
		created.LogoutCallbackURLs = []string{}
	}
	return created, nil
}

// ListClients returns every client with group grants attached.
func (s *PostgresStore) ListClients(ctx context.Context) ([]Client, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(clientColumns...)
	sb.From(oidcClientsTable + " c")
	sb.OrderBy("c.created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: list clients: %w", err)
	}
	defer rows.Close()

	clients := []Client{}
	for rows.Next() {
		c, scanErr := scanClient(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		clients = append(clients, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range clients {
		if err := s.attachGroups(ctx, &clients[i]); err != nil {
			return nil, err
		}
	}
	return clients, nil
}

// GetClient resolves one client by ID.
func (s *PostgresStore) GetClient(ctx context.Context, id OIDCClientID) (Client, error) {
	query, args := s.clientSelect(id.String()).Build()
	row, err := scanClient(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Client{}, ErrNotFound
		}
		return Client{}, fmt.Errorf("oidc store: get client: %w", err)
	}
	if err := s.attachGroups(ctx, row); err != nil {
		return Client{}, err
	}
	return *row, nil
}

// UpdateClient patches the row; nil fields keep their values.
func (s *PostgresStore) UpdateClient(ctx context.Context, id OIDCClientID, params ClientUpdateParams) (Client, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcClientsTable)

	assignments := []string{}
	if params.Name != nil {
		assignments = append(assignments, ub.Assign("name", *params.Name))
	}
	if params.Description != nil {
		assignments = append(assignments, ub.Assign("description", *params.Description))
	}
	if params.CallbackURLs != nil {
		callbacks, _ := json.Marshal(params.CallbackURLs)
		assignments = append(assignments, ub.Assign("callback_urls", callbacks))
	}
	if params.LogoutCallbackURLs != nil {
		logoutCallbacks, _ := json.Marshal(params.LogoutCallbackURLs)
		assignments = append(assignments, ub.Assign("logout_callback_urls", logoutCallbacks))
	}
	if params.LaunchURL != nil {
		assignments = append(assignments, ub.Assign("launch_url", *params.LaunchURL))
	}
	if params.IsPublic != nil {
		assignments = append(assignments, ub.Assign("is_public", *params.IsPublic))
	}
	if params.PKCEEnabled != nil {
		assignments = append(assignments, ub.Assign("pkce_enabled", *params.PKCEEnabled))
	}
	if params.PKCESupported != nil {
		assignments = append(assignments, ub.Assign("pkce_supported", *params.PKCESupported))
	}
	if params.SkipConsent != nil {
		assignments = append(assignments, ub.Assign("skip_consent", *params.SkipConsent))
	}
	if params.IsGroupRestricted != nil {
		assignments = append(assignments, ub.Assign("is_group_restricted", *params.IsGroupRestricted))
	}
	if params.AccessTokenDurationMinutes != nil {
		assignments = append(assignments, ub.Assign("access_token_duration_minutes", *params.AccessTokenDurationMinutes))
	}
	if params.RefreshTokenDurationMinutes != nil {
		assignments = append(assignments, ub.Assign("refresh_token_duration_minutes", *params.RefreshTokenDurationMinutes))
	}
	if params.SecretHash != nil {
		assignments = append(assignments, ub.Assign("secret", *params.SecretHash))
	}
	if len(assignments) == 0 {
		return s.GetClient(ctx, id)
	}

	ub.Set(assignments...)
	ub.Where(ub.E("id", id.String()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return Client{}, fmt.Errorf("oidc store: update client: %w", err)
	}
	return s.GetClient(ctx, id)
}

// DeleteClient removes a client (cascades to grants and codes).
func (s *PostgresStore) DeleteClient(ctx context.Context, id OIDCClientID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(oidcClientsTable)
	db.Where(db.E("id", id.String()))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: delete client: %w", err)
	}
	return nil
}

// userUUID converts a typed ID string (or bare UUID) to its UUID
// column form, staying decoupled from the identity packages.
func userUUID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}

// SetClientLogoPath stores or clears (nil) the logo blob path and
// keeps the upstream-compat image_type column in sync (the meta
// view's has_logo reads it).
func (s *PostgresStore) SetClientLogoPath(ctx context.Context, id OIDCClientID, logoPath *string) error {
	imageType := new(string)
	if logoPath != nil {
		*imageType = strings.TrimPrefix(path.Ext(*logoPath), ".")
	} else {
		imageType = nil
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcClientsTable)
	ub.Set(ub.Assign("logo_path", logoPath), ub.Assign("image_type", imageType))
	ub.Where(ub.E("id", id.String()))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("oidc store: set logo: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetClientGroups replaces the group allowlist for restricted
// clients. Group IDs arrive as typeid strings (or UUIDs).
func (s *PostgresStore) SetClientGroups(ctx context.Context, id OIDCClientID, groupIDs []string) error {
	deleter := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	deleter.DeleteFrom(oidcClientsAllowedGroupsTable)
	deleter.Where(deleter.E("oidc_client_id", id.String()))
	query, args := deleter.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: clear groups: %w", err)
	}

	for _, groupID := range groupIDs {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(oidcClientsAllowedGroupsTable)
		ib.Cols("oidc_client_id", "user_group_id")
		ib.Values(id.String(), userUUID(groupID))
		query, args := ib.Build()
		if _, err := s.exec.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("oidc store: grant group: %w", err)
		}
	}
	return nil
}

// attachGroups loads the allowlist onto a client.
func (s *PostgresStore) attachGroups(ctx context.Context, c *Client) error {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_group_id")
	sb.From(oidcClientsAllowedGroupsTable)
	sb.Where(sb.E("oidc_client_id", c.ID.String()))

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("oidc store: client groups: %w", err)
	}
	defer rows.Close()

	c.AllowedGroupIDs = []string{}
	for rows.Next() {
		var groupID string
		if err := rows.Scan(&groupID); err != nil {
			return err
		}
		c.AllowedGroupIDs = append(c.AllowedGroupIDs, groupID)
	}
	return rows.Err()
}

// InsertCode stores a one-time code (hashed) with its TTL.
func (s *PostgresStore) InsertCode(ctx context.Context, code AuthorizationCode) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oidcAuthorizationCodesTable)
	ib.Cols("code", "scope", "nonce", "code_challenge", "code_challenge_method_sha256", "authentication_method", "user_id", "client_id", "expires_at")
	ib.Values(code.CodeHash, code.Scope, code.Nonce, code.CodeChallenge, code.MethodSHA256, code.AuthMethod, code.UserID, code.ClientID, code.ExpiresAt)

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: insert code: %w", err)
	}
	return nil
}

// ConsumeCode deletes and returns the code row atomically — one-time
// use is enforced by the DELETE itself.
func (s *PostgresStore) ConsumeCode(ctx context.Context, codeHash string) (AuthorizationCode, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(oidcAuthorizationCodesTable)
	db.Where(db.And(
		db.E("code", codeHash),
		db.GT("expires_at", time.Now().UTC()),
	))
	db.Returning("code", "scope", "nonce", "code_challenge", "code_challenge_method_sha256", "authentication_method", "user_id", "client_id", "expires_at")

	query, args := db.Build()
	var code AuthorizationCode
	var nonce *string
	var challenge *string
	var expiresAt pgtype.Timestamptz
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&code.CodeHash, &code.Scope, &nonce, &challenge, &code.MethodSHA256, &code.AuthMethod, &code.UserID, &code.ClientID, &expiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return code, ErrInvalidGrant
		}
		return code, fmt.Errorf("oidc store: consume code: %w", err)
	}
	if nonce != nil {
		code.Nonce = *nonce
	}
	if challenge != nil {
		code.CodeChallenge = *challenge
	}
	code.ExpiresAt = expiresAt.Time
	return code, nil
}

// PutSession upserts one oauth2_sessions row (kind, key) unique.
func (s *PostgresStore) PutSession(ctx context.Context, session OAuth2Session) error {
	requestData, err := json.Marshal(session.RequestData)
	if err != nil {
		return fmt.Errorf("oidc store: marshal request data: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oauth2SessionsTable)
	ib.Cols("kind", "key", "request_id", "active", "request_data", "client_id", "expires_at")
	ib.Values(session.Kind, session.Key, session.RequestID, session.Active, requestData, session.ClientID, session.ExpiresAt)
	ib.SQL("ON CONFLICT (kind, key) DO NOTHING")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: put session: %w", err)
	}
	return nil
}

// GetSession resolves one session row by kind + key.
func (s *PostgresStore) GetSession(ctx context.Context, kind, key string) (OAuth2Session, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("request_id", "active", "request_data", "client_id", "expires_at", "created_at")
	sb.From(oauth2SessionsTable)
	sb.Where(sb.And(sb.E("kind", kind), sb.E("key", key)))

	query, args := sb.Build()
	var session OAuth2Session
	var requestData []byte
	var expiresAt, createdAt pgtype.Timestamptz
	session.Kind = kind
	session.Key = key
	err := s.exec.QueryRow(ctx, query, args...).Scan(&session.RequestID, &session.Active, &requestData, &session.ClientID, &expiresAt, &createdAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return session, ErrInvalidGrant
		}
		return session, fmt.Errorf("oidc store: get session: %w", err)
	}
	if err := json.Unmarshal(requestData, &session.RequestData); err != nil {
		return session, fmt.Errorf("oidc store: unmarshal request data: %w", err)
	}
	if expiresAt.Valid {
		session.ExpiresAt = &expiresAt.Time
	}
	if createdAt.Valid {
		session.CreatedAt = &createdAt.Time
	}
	return session, nil
}

// DeactivateSession flips one session row inactive (single-rotation
// refresh).
func (s *PostgresStore) DeactivateSession(ctx context.Context, kind, key string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oauth2SessionsTable)
	ub.Set(ub.Assign("active", false))
	ub.Where(ub.And(ub.E("kind", kind), ub.E("key", key)))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: deactivate session: %w", err)
	}
	return nil
}

// DeactivateFamily revokes every active session row in a token
// family — refresh-token reuse kills the whole grant.
func (s *PostgresStore) DeactivateFamily(ctx context.Context, requestID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oauth2SessionsTable)
	ub.Set(ub.Assign("active", false))
	ub.Where(ub.E("request_id", requestID))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: deactivate family: %w", err)
	}
	return nil
}

// RecordJTI registers a replayed token ID until it expires.
func (s *PostgresStore) RecordJTI(ctx context.Context, jti string, expiresAt time.Time) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oauth2JtisTable)
	ib.Cols("jti", "expires_at")
	ib.Values(jti, expiresAt)
	ib.SQL("ON CONFLICT (jti) DO NOTHING")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: record jti: %w", err)
	}
	return nil
}

// JTIExists reports whether jti was already spent.
func (s *PostgresStore) JTIExists(ctx context.Context, jti string) (bool, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(oauth2JtisTable)
	sb.Where(sb.E("jti", jti))

	query, args := sb.Build()
	var count int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return false, fmt.Errorf("oidc store: jti lookup: %w", err)
	}
	return count > 0, nil
}

// UpsertAuthorizedClient remembers the scopes a user granted.
func (s *PostgresStore) UpsertAuthorizedClient(ctx context.Context, userID, clientID string, scopes []string) error {
	scopeJSON, err := json.Marshal(scopes)
	if err != nil {
		return fmt.Errorf("oidc store: marshal scopes: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(userAuthorizedClientsTable)
	ib.Cols("user_id", "client_id", "scope", "last_used_at")
	ib.Values(userUUID(userID), clientID, scopeJSON, time.Now().UTC())
	ib.SQL("ON CONFLICT (user_id, client_id) DO UPDATE SET scope = EXCLUDED.scope, last_used_at = EXCLUDED.last_used_at")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: upsert authorized client: %w", err)
	}
	return nil
}

// CreateInteraction inserts an interaction row (state machine
// seed).
func (s *PostgresStore) CreateInteraction(ctx context.Context, session InteractionSession) error {
	parameters, err := json.Marshal(session.Parameters)
	if err != nil {
		return fmt.Errorf("oidc store: marshal parameters: %w", err)
	}
	scopes, _ := json.Marshal(session.Scopes)

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(interactionSessionsTable)
	ib.Cols("id", "consent_required", "reauthentication_required", "authentication_required", "account_selection_required", "scopes", "client_id", "user_id", "requested_at", "parameters")

	var userID any
	if session.UserID != nil {
		userID = userUUID(*session.UserID)
	}
	ib.Values(
		session.ID.UUID(), session.ConsentRequired, false, session.AuthenticationRequired, false,
		scopes, session.ClientID, userID, session.RequestedAt, parameters,
	)

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: create interaction: %w", err)
	}
	return nil
}

// GetInteraction resolves one interaction row.
func (s *PostgresStore) GetInteraction(ctx context.Context, id InteractionSessionID) (InteractionSession, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("consent_required", "authentication_required", "scopes", "client_id", "user_id", "requested_at", "parameters")
	sb.From(interactionSessionsTable)
	sb.Where(sb.E("id", id.UUID()))

	query, args := sb.Build()
	var (
		session     InteractionSession
		scopes      []byte
		parameters  []byte
		userID      *string
		clientID    string
		requestedAt pgtype.Timestamptz
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&session.ConsentRequired, &session.AuthenticationRequired, &scopes, &clientID, &userID, &requestedAt, &parameters,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return session, ErrNotFound
		}
		return session, fmt.Errorf("oidc store: get interaction: %w", err)
	}

	session.ID = id
	session.ClientID = clientID
	session.UserID = userID
	session.RequestedAt = requestedAt.Time
	_ = json.Unmarshal(scopes, &session.Scopes)
	if err := json.Unmarshal(parameters, &session.Parameters); err != nil {
		session.Parameters = map[string]any{}
	}
	return session, nil
}

// UpdateInteraction links the signed-in user / flips consent.
func (s *PostgresStore) UpdateInteraction(ctx context.Context, id InteractionSessionID, userID *string, consentRequired *bool) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(interactionSessionsTable)

	assignments := []string{}
	if userID != nil {
		assignments = append(assignments, ub.Assign("user_id", userUUID(*userID)))
	}
	if consentRequired != nil {
		assignments = append(assignments, ub.Assign("consent_required", *consentRequired))
	}
	if len(assignments) == 0 {
		return nil
	}

	ub.Set(assignments...)
	ub.Where(ub.E("id", id.UUID()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: update interaction: %w", err)
	}
	return nil
}

// DeleteInteraction drops a finished interaction.
func (s *PostgresStore) DeleteInteraction(ctx context.Context, id InteractionSessionID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(interactionSessionsTable)
	db.Where(db.E("id", id.UUID()))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: delete interaction: %w", err)
	}
	return nil
}

// UserInGroup checks membership (restricted-client allowlist).
func (s *PostgresStore) UserInGroup(ctx context.Context, userID, groupID string) bool {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("count(*)")
	sb.From(userGroupsUsersTable)
	sb.Where(sb.And(sb.E("user_id", userUUID(userID)), sb.E("user_group_id", groupID)))

	query, args := sb.Build()
	var count int
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

// UserByID reads the token-claim source user row.
func (s *PostgresStore) UserByID(ctx context.Context, userID string) (UserProfile, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "username", "email", "display_name", "email_verified_at", "disabled")
	sb.From(usersTable)
	sb.Where(sb.E("id", userUUID(userID)))

	query, args := sb.Build()
	var (
		profile    UserProfile
		verifiedAt pgtype.Timestamptz
		disabled   bool
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(
		&profile.ID, &profile.Username, &profile.Email, &profile.DisplayName, &verifiedAt, &disabled,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return profile, ErrInvalidGrant
		}
		return profile, fmt.Errorf("oidc store: user by id: %w", err)
	}
	if verifiedAt.Valid {
		profile.EmailVerifiedAt = &verifiedAt.Time
	}
	profile.Disabled = disabled
	return profile, nil
}

// UserGroups lists group names for the groups claim.
func (s *PostgresStore) UserGroups(ctx context.Context, userID string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("g.name")
	sb.From(userGroupsTable + " g")
	sb.Join(userGroupsUsersTable + " m ON m.user_group_id = g.id")
	sb.Where(sb.E("m.user_id", userUUID(userID)))
	sb.OrderBy("g.name")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: user groups: %w", err)
	}
	defer rows.Close()

	groups := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		groups = append(groups, name)
	}
	return groups, rows.Err()
}

// CustomClaims merges user-scoped and group-scoped claims.
func (s *PostgresStore) CustomClaims(ctx context.Context, userID string) (map[string]string, error) {
	groupIDs, err := s.userGroupIDs(ctx, userID)
	if err != nil {
		return nil, err
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("key", "value")
	sb.From(customClaimsTable)

	condition := sb.E("user_id", userUUID(userID))
	if len(groupIDs) > 0 {
		condition = sb.Or(condition, sb.In("user_group_id", sqlbuilder.List(groupIDs)))
	}
	sb.Where(condition)

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: custom claims: %w", err)
	}
	defer rows.Close()

	claims := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		claims[key] = value
	}
	return claims, rows.Err()
}

// userGroupIDs collects the user's group UUIDs for claim queries.
func (s *PostgresStore) userGroupIDs(ctx context.Context, userID string) ([]string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("user_group_id")
	sb.From(userGroupsUsersTable)
	sb.Where(sb.E("user_id", userUUID(userID)))

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: user group ids: %w", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// nullIfEmpty maps "" to nil for nullable columns.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// AccessibleClients lists the clients a user may reach: everything
// unrestricted plus restricted clients where the user is in at
// least one allowed group.
func (s *PostgresStore) AccessibleClients(ctx context.Context, userID string) ([]Client, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(clientColumns...)
	sb.From(oidcClientsTable + " c")
	sb.Where(sb.Or(
		sb.E("c.is_group_restricted", false),
		"EXISTS (SELECT 1 FROM "+oidcClientsAllowedGroupsTable+" g JOIN "+userGroupsUsersTable+" m ON m.user_group_id = g.user_group_id WHERE g.oidc_client_id = c.id AND m.user_id = "+sb.Var(userUUID(userID))+")",
	))
	sb.OrderBy("c.created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: accessible clients: %w", err)
	}
	defer rows.Close()

	clients := []Client{}
	for rows.Next() {
		c, scanErr := scanClient(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		clients = append(clients, *c)
	}
	return clients, rows.Err()
}

// AuthorizedClients lists consent records; a nil userID means all
// users (admin view).
func (s *PostgresStore) AuthorizedClients(ctx context.Context, userID *string) ([]AuthorizedClient, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("a.user_id", "a.client_id", "a.scope", "a.last_used_at")
	sb.From(userAuthorizedClientsTable + " a")
	if userID != nil {
		sb.Where(sb.E("a.user_id", userUUID(*userID)))
	}
	sb.OrderBy("a.last_used_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("oidc store: authorized clients: %w", err)
	}
	defer rows.Close()

	out := []AuthorizedClient{}
	for rows.Next() {
		record, scanErr := scanAuthorizedClient(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *record)
	}
	return out, rows.Err()
}

// scanAuthorizedClient scans one consent row.
func scanAuthorizedClient(row scanner) (*AuthorizedClient, error) {
	var (
		record   AuthorizedClient
		scopeRaw []byte
		lastUsed pgtype.Timestamptz
	)
	if err := row.Scan(&record.UserID, &record.ClientID, &scopeRaw, &lastUsed); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(scopeRaw, &record.Scopes); err != nil {
		record.Scopes = []string{}
	}
	record.LastUsedAt = lastUsed.Time
	return &record, nil
}

// DeleteAuthorization drops the consent row.
func (s *PostgresStore) DeleteAuthorization(ctx context.Context, userID, clientID string) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(userAuthorizedClientsTable)
	db.Where(db.And(db.E("user_id", userUUID(userID)), db.E("client_id", clientID)))

	query, args := db.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: delete authorization: %w", err)
	}
	return nil
}

// RevokeClientTokens deactivates every active token session row of
// one user for one client (authorization revocation cascade).
func (s *PostgresStore) RevokeClientTokens(ctx context.Context, clientID, userID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oauth2SessionsTable)
	ub.Set(ub.Assign("active", false))
	ub.Where(ub.And(
		ub.E("client_id", clientID),
		ub.E("active", true),
		"request_data->>'subject' = "+ub.Var(userUUID(userID)),
	))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: revoke client tokens: %w", err)
	}
	return nil
}

// AddClientSecret appends a credentials entry. The legacy
// single-secret column mirrors the FIRST entry only (older
// readers).
func (s *PostgresStore) AddClientSecret(ctx context.Context, clientID OIDCClientID, entry ClientSecret, rawHash string) error {
	credentials, err := s.credentialsJSON(ctx, clientID)
	if err != nil {
		return err
	}
	entry.SecretHash = rawHash
	credentials = append(credentials, entry)
	encoded, marshalErr := json.Marshal(credentials)
	if marshalErr != nil {
		return fmt.Errorf("oidc store: marshal credentials: %w", marshalErr)
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcClientsTable)
	assignments := []string{ub.Assign("credentials", encoded)}
	if len(credentials) == 1 {
		assignments = append(assignments, ub.Assign("secret", rawHash))
	}
	ub.Set(assignments...)
	ub.Where(ub.E("id", clientID.String()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: add secret: %w", err)
	}
	return nil
}

// DeleteClientSecret removes one credentials entry; the legacy
// column clears when the list empties or the removed entry was the
// legacy hash.
func (s *PostgresStore) DeleteClientSecret(ctx context.Context, clientID OIDCClientID, secretID string) error {
	credentials, err := s.credentialsJSON(ctx, clientID)
	if err != nil {
		return err
	}

	kept := credentials[:0]
	var legacyHash *string
	for _, entry := range credentials {
		if entry.ID == secretID {
			if entry.ID == LegacySecretID {
				hash := entry.SecretHash
				legacyHash = &hash
			}
			continue
		}
		kept = append(kept, entry)
	}
	encoded, marshalErr := json.Marshal(kept)
	if marshalErr != nil {
		return fmt.Errorf("oidc store: marshal credentials: %w", marshalErr)
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcClientsTable)
	assignments := []string{ub.Assign("credentials", encoded)}
	if len(kept) == 0 || legacyHash != nil {
		assignments = append(assignments, ub.Assign("secret", nil))
	}
	ub.Set(assignments...)
	ub.Where(ub.E("id", clientID.String()))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: delete secret: %w", err)
	}
	return nil
}

// credentialsJSON reads the current credentials list.
func (s *PostgresStore) credentialsJSON(ctx context.Context, clientID OIDCClientID) ([]ClientSecret, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("credentials")
	sb.From(oidcClientsTable)
	sb.Where(sb.E("id", clientID.String()))

	query, args := sb.Build()
	var credentials []byte
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&credentials); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("oidc store: read credentials: %w", err)
	}

	var credentialsList []ClientSecret
	_ = json.Unmarshal(credentials, &credentialsList)
	return credentialsList, nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanClient scans one row; keep in sync with clientColumns. The
// id column stores the typeid string; secrets carry SHA-256 hashes
// (legacy single-secret column + credentials JSONB list).
func scanClient(row scanner) (*Client, error) {
	var (
		id          string
		c           Client
		name        *string
		secret      *string
		credentials []byte
		callbacks   []byte
		logoutCBs   []byte
		launchURL   *string
		createdByID *string
		createdAt   pgtype.Timestamptz
	)
	if err := row.Scan(
		&id, &name, &c.Description, &secret, &credentials, &callbacks, &logoutCBs, &launchURL,
		&c.IsPublic, &c.PKCEEnabled, &c.PKCESupported, &c.RequiresReauthentication, &c.SkipConsent, &c.IsGroupRestricted,
		&c.AccessTokenDurationMinutes, &c.RefreshTokenDurationMinutes, &createdByID, &createdAt,
		&c.ImageType, &c.DarkImageType, &c.ClientType, &c.LogoPath,
	); err != nil {
		return nil, err
	}

	clientID, err := typeid.Parse[OIDCClientID](id)
	if err != nil {
		return nil, fmt.Errorf("oidc store: client id %q is not a typeid: %w", id, err)
	}
	c.ID = clientID
	if name != nil {
		c.Name = *name
	}
	if secret != nil {
		c.SecretHash = secret
	}
	_ = json.Unmarshal(credentials, &c.Secrets)
	if c.Secrets == nil && secret != nil {
		// Secrets migrated before the credentials list existed show
		// up as one synthetic legacy entry.
		c.Secrets = []ClientSecret{{
			ID:         LegacySecretID,
			SecretHash: *secret,
			IsActive:   true,
		}}
	}
	if launchURL != nil {
		c.LaunchURL = *launchURL
	}
	if createdByID != nil {
		c.CreatedByID = createdByID
	}
	_ = json.Unmarshal(callbacks, &c.CallbackURLs)
	_ = json.Unmarshal(logoutCBs, &c.LogoutCallbackURLs)
	if c.CallbackURLs == nil {
		c.CallbackURLs = []string{}
	}
	if c.LogoutCallbackURLs == nil {
		c.LogoutCallbackURLs = []string{}
	}
	if c.ClientType == "" {
		c.ClientType = "standard"
	}
	c.CreatedAt = createdAt.Time
	return &c, nil
}
