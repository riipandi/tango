package emailverification

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// PostgresStore persists tokens in public.auth_tokens.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{exec: store}
}

// tokenColumns is the SELECT list; keep in sync with scanToken.
var tokenColumns = []string{"id", "user_id", "token_hash", "created_at", "expires_at"}

// Upsert writes (or replaces) the single token per user+purpose.
func (s *PostgresStore) Upsert(ctx context.Context, token *Token) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(authTokensTable)
	ib.Cols("user_id", "token_hash", "purpose", "expires_at")
	ib.Values(userUUID(token.UserID), token.TokenHash, purpose, token.ExpiresAt)
	ib.SQL("ON CONFLICT (user_id, purpose) DO UPDATE SET token_hash = EXCLUDED.token_hash, expires_at = EXCLUDED.expires_at")
	ib.Returning(tokenColumns...)

	query, args := ib.Build()
	row, err := scanToken(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		return fmt.Errorf("emailverification store: upsert: %w", err)
	}
	*token = *row
	return nil
}

// Consume deletes + returns the token for the hash.
func (s *PostgresStore) Consume(ctx context.Context, tokenHash string) (*Token, error) {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(authTokensTable)
	db.Where(db.And(
		db.E("token_hash", tokenHash),
		db.E("purpose", purpose),
		db.GT("expires_at", time.Now().UTC()),
	))
	db.Returning(tokenColumns...)

	query, args := db.Build()
	row, err := scanToken(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("emailverification store: consume: %w", err)
	}
	return row, nil
}

// scanToken scans one row; keep in sync with tokenColumns.
func scanToken(row scanner) (*Token, error) {
	var (
		id      string
		userID  string
		t       Token
		created time.Time
		expires time.Time
	)
	if err := row.Scan(&id, &userID, &t.TokenHash, &created, &expires); err != nil {
		return nil, err
	}

	parsed, err := typeid.FromUUID[tokenID](id)
	if err != nil {
		return nil, fmt.Errorf("emailverification store: token id %q is not a UUID: %w", id, err)
	}
	t.ID = parsed.String()
	t.UserID = userUUID(userID)
	t.CreatedAt = created
	t.ExpiresAt = expires
	return &t, nil
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

// userUUID normalizes a typed ID string (or bare UUID) to the UUID
// column form.
func userUUID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}
