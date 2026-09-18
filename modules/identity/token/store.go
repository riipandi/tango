package token

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// PostgresStore persists tokens in public.auth_tokens, scoped to one
// purpose per instance.
type PostgresStore struct {
	exec    datastore.Executor
	purpose Purpose
}

var _ Store = (*PostgresStore)(nil)

// NewStore builds the production store for one purpose.
func NewStore(exec datastore.Executor, purpose Purpose) *PostgresStore {
	return &PostgresStore{exec: exec, purpose: purpose}
}

// tokenColumns is the SELECT list; keep in sync with scanToken.
var tokenColumns = []string{"id", "user_id", "token_hash", "created_at", "expires_at", "last_sent_at"}

// Upsert writes (or replaces) the single token per user+purpose and
// stamps last_sent_at.
func (s *PostgresStore) Upsert(ctx context.Context, token *Token) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(authTokensTable)
	ib.Cols("user_id", "token_hash", "purpose", "expires_at", "last_sent_at")
	ib.Values(datastore.UserUUID(token.UserID), token.TokenHash, string(s.purpose), token.ExpiresAt, time.Now().UTC())
	ib.SQL("ON CONFLICT (user_id, purpose) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at, last_sent_at = EXCLUDED.last_sent_at")
	ib.Returning(tokenColumns...)

	query, args := ib.Build()
	row, err := scanToken(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return fmt.Errorf("token store: upsert: %w", err)
	}
	*token = *row
	return nil
}

// Consume deletes + returns the token for the hash; only an
// unexpired token resolves.
func (s *PostgresStore) Consume(ctx context.Context, tokenHash string) (*Token, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(authTokensTable)
	db.Where(db.And(
		db.E("token_hash", tokenHash),
		db.E("purpose", string(s.purpose)),
		db.GT("expires_at", time.Now().UTC()),
	))
	db.Returning(tokenColumns...)

	query, args := db.Build()
	row, err := scanToken(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("token store: consume: %w", err)
	}
	return row, nil
}

// scanToken scans one row; keep in sync with tokenColumns.
func scanToken(row scanner) (*Token, error) {
	var (
		id       string
		userID   string
		t        Token
		created  pgtype.Timestamptz
		expires  pgtype.Timestamptz
		lastSent pgtype.Timestamptz
	)
	if err := row.Scan(&id, &userID, &t.TokenHash, &created, &expires, &lastSent); err != nil {
		return nil, err
	}

	parsed, err := typeid.FromUUID[tokenID](id)
	if err != nil {
		return nil, fmt.Errorf("token store: token id %q is not a UUID: %w", id, err)
	}
	t.ID = parsed.String()
	t.UserID = userID
	t.CreatedAt = created.Time
	t.ExpiresAt = expires.Time
	if lastSent.Valid {
		t.LastSentAt = &lastSent.Time
	}
	return &t, nil
}

// hashToken hashes a raw token for at-rest storage.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// tokenID is a local typeid alias (row ID not URL-facing).
type tokenID = typeid.TypeID[tokenPrefix]

type tokenPrefix struct{}

func (tokenPrefix) Prefix() string { return "auth_token" }

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// authTokensTable is the shared token table.
const authTokensTable = "public.auth_tokens"
