package oidc

import (
	"context"
	"errors"
	"fmt"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

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
	// KindPAR is a pushed authorization request (RFC 9126): the key
	// is the request_uri payload, unique per push.
	KindPAR = "par"
)

// PutSession upserts one oauth2_sessions row (kind, key) unique.
func (s *PostgresStore) PutSession(ctx context.Context, session OAuth2Session) error {
	requestData, err := jsonv2.Marshal(session.RequestData)
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
	if err := jsonv2.Unmarshal(requestData, &session.RequestData); err != nil {
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
