package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/riipandi/tango/internal/datastore"
)

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

// UpsertAuthorizedClient remembers the scopes a user granted.
func (s *PostgresStore) UpsertAuthorizedClient(ctx context.Context, userID, clientID string, scopes []string) error {
	scopeJSON, err := json.Marshal(scopes)
	if err != nil {
		return fmt.Errorf("oidc store: marshal scopes: %w", err)
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(userAuthorizedClientsTable)
	ib.Cols("user_id", "client_id", "scope", "last_used_at")
	ib.Values(datastore.UserUUID(userID), clientID, scopeJSON, time.Now().UTC())
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
		userID = datastore.UserUUID(*session.UserID)
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
		assignments = append(assignments, ub.Assign("user_id", datastore.UserUUID(*userID)))
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

// AuthorizedClients lists consent records; a nil userID means all
// users (admin view).
func (s *PostgresStore) AuthorizedClients(ctx context.Context, userID *string) ([]AuthorizedClient, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("a.user_id", "a.client_id", "a.scope", "a.last_used_at")
	sb.From(userAuthorizedClientsTable + " a")
	if userID != nil {
		sb.Where(sb.E("a.user_id", datastore.UserUUID(*userID)))
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
	db.Where(db.And(db.E("user_id", datastore.UserUUID(userID)), db.E("client_id", clientID)))

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
		"request_data->>'subject' = "+ub.Var(datastore.UserUUID(userID)),
	))

	query, args := ub.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("oidc store: revoke client tokens: %w", err)
	}
	return nil
}
