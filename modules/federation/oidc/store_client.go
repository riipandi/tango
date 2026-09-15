package oidc

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

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
	MetadataURL                 *string
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
	MetadataGrantTypes          []string
	MetadataExpiresAt           *time.Time
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
	MetadataURL                 string
	ClientType                  string
	MetadataGrantTypes          []string
	MetadataExpiresAt           *time.Time
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
	RequiresReauthentication    *bool
	AccessTokenDurationMinutes  *int64
	RefreshTokenDurationMinutes *int64
	SecretHash                  *string
	MetadataGrantTypes          []string
}

// clientColumns is the SELECT list; keep in sync with scanClient.
var clientColumns = []string{
	"c.id", "c.name", "c.description", "c.secret", "c.credentials", "c.callback_urls", "c.logout_callback_urls",
	"c.launch_url", "c.is_public", "c.pkce_enabled", "c.pkce_supported",
	"c.requires_reauthentication", "c.skip_consent", "c.is_group_restricted",
	"c.access_token_duration_minutes", "c.refresh_token_duration_minutes",
	"c.created_by_id", "c.created_at", "c.image_type", "c.dark_image_type", "c.client_type", "c.logo_path", "c.metadata_url",
	"c.metadata_grant_types", "c.metadata_expires_at",
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
	callbacks, _ := jsonv2.Marshal(params.CallbackURLs)
	logoutCallbacks, _ := jsonv2.Marshal(params.LogoutCallbackURLs)

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(oidcClientsTable)
	ib.Cols(
		"id", "name", "description", "secret", "callback_urls", "logout_callback_urls", "launch_url",
		"is_public", "pkce_enabled", "pkce_supported", "skip_consent", "is_group_restricted",
		"access_token_duration_minutes", "refresh_token_duration_minutes", "created_by_id",
		"client_type", "metadata_url",
	)
	ib.Values(
		id.String(), params.Name, params.Description, nullIfEmpty(params.SecretHash), callbacks, logoutCallbacks, params.LaunchURL,
		params.IsPublic, params.PKCEEnabled, params.PKCESupported, params.SkipConsent, params.IsGroupRestricted,
		params.AccessTokenDurationMinutes, params.RefreshTokenDurationMinutes, nullIfEmpty(params.CreatedByID),
		orDefault(params.ClientType, "standard"), nullIfEmpty(params.MetadataURL),
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
	// Metadata-owned columns (name + redirect URIs) are never written
	// from an admin snapshot for CIMD clients — a refresh may have
	// changed them after this request read its copy (upstream:
	// CIMDDoesNotOverwriteConcurrentMetadataRefresh).
	metadataOwned := false
	if row, err := s.GetClient(ctx, id); err == nil && row.ClientType == "cimd" {
		metadataOwned = true
	}
	if params.Name != nil && !metadataOwned {
		assignments = append(assignments, ub.Assign("name", *params.Name))
	}
	if params.Description != nil {
		assignments = append(assignments, ub.Assign("description", *params.Description))
	}
	if params.CallbackURLs != nil && !metadataOwned {
		callbacks, _ := jsonv2.Marshal(params.CallbackURLs)
		assignments = append(assignments, ub.Assign("callback_urls", callbacks))
	}
	if params.LogoutCallbackURLs != nil && !metadataOwned {
		logoutCallbacks, _ := jsonv2.Marshal(params.LogoutCallbackURLs)
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

// RefreshClientMetadata writes the document-owned columns for a CIMD
// client after a re-fetch (the refresh endpoint; admin updates may
// never touch these — see UpdateClient's guard).
func (s *PostgresStore) RefreshClientMetadata(ctx context.Context, id OIDCClientID, params ClientUpdateParams) error {
	callbacks, _ := jsonv2.Marshal(params.CallbackURLs)
	logoutCallbacks, _ := jsonv2.Marshal(params.LogoutCallbackURLs)
	grants, _ := jsonv2.Marshal(params.MetadataGrantTypes)

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(oidcClientsTable)
	ub.Set(
		ub.Assign("name", datastore.Deref(params.Name)),
		ub.Assign("description", datastore.Deref(params.Description)),
		ub.Assign("callback_urls", callbacks),
		ub.Assign("logout_callback_urls", logoutCallbacks),
		ub.Assign("launch_url", datastore.Deref(params.LaunchURL)),
		ub.Assign("is_public", datastore.Deref(params.IsPublic)),
		ub.Assign("skip_consent", datastore.Deref(params.SkipConsent)),
		ub.Assign("requires_reauthentication", datastore.Deref(params.RequiresReauthentication)),
		ub.Assign("is_group_restricted", datastore.Deref(params.IsGroupRestricted)),
		ub.Assign("access_token_duration_minutes", datastore.Deref(params.AccessTokenDurationMinutes)),
		ub.Assign("refresh_token_duration_minutes", datastore.Deref(params.RefreshTokenDurationMinutes)),
		ub.Assign("metadata_grant_types", grants),
		ub.Assign("metadata_expires_at", time.Now().UTC().Add(MetadataDocumentTTL)),
	)
	ub.Where(ub.E("id", id.String()))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("oidc store: refresh metadata: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
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
		ib.Values(id.String(), datastore.UserUUID(groupID))
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
	encoded, marshalErr := jsonv2.Marshal(credentials)
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
	encoded, marshalErr := jsonv2.Marshal(kept)
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
	_ = jsonv2.Unmarshal(credentials, &credentialsList)
	return credentialsList, nil
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
		"EXISTS (SELECT 1 FROM "+oidcClientsAllowedGroupsTable+" g JOIN "+userGroupsUsersTable+" m ON m.user_group_id = g.user_group_id WHERE g.oidc_client_id = c.id AND m.user_id = "+sb.Var(datastore.UserUUID(userID))+")",
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

		metadataGrants    []byte
		metadataExpiresAt pgtype.Timestamptz
	)
	if err := row.Scan(
		&id, &name, &c.Description, &secret, &credentials, &callbacks, &logoutCBs, &launchURL,
		&c.IsPublic, &c.PKCEEnabled, &c.PKCESupported, &c.RequiresReauthentication, &c.SkipConsent, &c.IsGroupRestricted,
		&c.AccessTokenDurationMinutes, &c.RefreshTokenDurationMinutes, &createdByID, &createdAt,
		&c.ImageType, &c.DarkImageType, &c.ClientType, &c.LogoPath, &c.MetadataURL, &metadataGrants, &metadataExpiresAt,
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
	_ = jsonv2.Unmarshal(credentials, &c.Secrets)
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
	_ = jsonv2.Unmarshal(callbacks, &c.CallbackURLs)
	_ = jsonv2.Unmarshal(logoutCBs, &c.LogoutCallbackURLs)
	if c.CallbackURLs == nil {
		c.CallbackURLs = []string{}
	}
	if c.LogoutCallbackURLs == nil {
		c.LogoutCallbackURLs = []string{}
	}
	if c.ClientType == "" {
		c.ClientType = "standard"
	}
	_ = jsonv2.Unmarshal(metadataGrants, &c.MetadataGrantTypes)
	if metadataExpiresAt.Valid {
		c.MetadataExpiresAt = &metadataExpiresAt.Time
	}
	c.CreatedAt = createdAt.Time
	return &c, nil
}
