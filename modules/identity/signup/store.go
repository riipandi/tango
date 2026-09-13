package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// Policy + TTL constants.
const (
	// TokenTTL bounds a signup token's validity.
	TokenTTL = 24 * time.Hour
	// UsageLimitMax caps tokens per create call.
	UsageLimitMax = 100

	signupTokensTable      = "public.signup_tokens"
	signupTokenGroupsTable = "public.signup_tokens_user_groups"
)

// Errors surfaced to handlers.
var (
	// ErrNotFound covers unknown/expired tokens without leaking.
	ErrNotFound = errors.New("signup: token is invalid or expired")
	// ErrExhausted covers a fully used token.
	ErrExhausted = errors.New("signup: token usage limit reached")
	// ErrInvalidIDs rejects unknown group IDs.
	ErrInvalidIDs = errors.New("signup: unknown user group id")
)

// SignupToken is one signup_tokens row; only create calls see the
// raw value.
type SignupToken struct {
	ID         SignupTokenID
	TokenHash  string
	UsageLimit int
	UsageCount int
	CreatedAt  time.Time
	ExpiresAt  time.Time
	GroupIDs   []string
}

// CreateParams carries admin-supplied token fields.
type CreateParams struct {
	TTL        time.Duration
	UsageLimit int
	GroupIDs   []string
}

// Store persists signup tokens.
type Store interface {
	// Create inserts a token (hash at rest) with its group grants.
	Create(ctx context.Context, params CreateParams, rawHash string) (*SignupToken, error)
	// List returns every token, newest first.
	List(ctx context.Context) ([]SignupToken, error)
	// Delete removes one token.
	Delete(ctx context.Context, id SignupTokenID) error
	// PeekValid resolves a token by hash; exhausted/expired fail.
	PeekValid(ctx context.Context, rawHash string) (*SignupToken, error)
	// ConsumeUse increments usage atomically; exhausted tokens fail.
	ConsumeUse(ctx context.Context, id SignupTokenID) error
}

// PostgresStore implements Store over the shared pool.
type PostgresStore struct {
	store datastore.Store
	exec  datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production store.
func NewPostgresStore(store datastore.Store) *PostgresStore {
	return &PostgresStore{store: store, exec: store}
}

// tokenColumns is the SELECT list; keep in sync with scanToken.
var tokenColumns = []string{"t.id", "t.token_hash", "t.usage_limit", "t.usage_count", "t.created_at", "t.expires_at"}

// Create inserts a token + group grants in one transaction.
func (s *PostgresStore) Create(ctx context.Context, params CreateParams, rawHash string) (*SignupToken, error) {
	var created *SignupToken
	err := s.store.WithTx(ctx, func(tx datastore.Executor) error {
		ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
		ib.InsertInto(signupTokensTable)
		ib.Cols("token_hash", "usage_limit", "expires_at")
		ib.Values(rawHash, params.UsageLimit, time.Now().UTC().Add(params.TTL))
		ib.Returning("id", "created_at", "expires_at")

		query, args := ib.Build()
		created = &SignupToken{
			TokenHash:  rawHash,
			UsageLimit: params.UsageLimit,
			GroupIDs:   []string{},
		}
		var (
			rowID      string
			rowCreated pgtype.Timestamptz
			rowExpires pgtype.Timestamptz
		)
		if scanErr := tx.QueryRow(ctx, query, args...).Scan(&rowID, &rowCreated, &rowExpires); scanErr != nil {
			return fmt.Errorf("signup store: insert: %w", scanErr)
		}

		parsed, parseErr := typeid.FromUUID[SignupTokenID](rowID)
		if parseErr != nil {
			return fmt.Errorf("signup store: token id %q is not a UUID: %w", rowID, parseErr)
		}
		created.ID = parsed
		created.CreatedAt = rowCreated.Time
		created.ExpiresAt = rowExpires.Time

		for _, groupID := range params.GroupIDs {
			jb := sqlbuilder.PostgreSQL.NewInsertBuilder()
			jb.InsertInto(signupTokenGroupsTable)
			jb.Cols("signup_token_id", "user_group_id")
			jb.Values(created.ID.UUIDBytes(), groupID)
			jQuery, jArgs := jb.Build()
			if _, jbErr := tx.Exec(ctx, jQuery, jArgs...); jbErr != nil {
				return fmt.Errorf("signup store: grant group: %w", jbErr)
			}
			created.GroupIDs = append(created.GroupIDs, groupID)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

// List returns every token, newest first, with group grants.
func (s *PostgresStore) List(ctx context.Context) ([]SignupToken, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(tokenColumns...)
	sb.From(signupTokensTable + " t")
	sb.OrderBy("t.created_at DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("signup store: list: %w", err)
	}
	defer rows.Close()

	out := []SignupToken{}
	for rows.Next() {
		row, scanErr := scanToken(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// Delete removes one token.
func (s *PostgresStore) Delete(ctx context.Context, id SignupTokenID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(signupTokensTable)
	db.Where(db.E("id", id.UUIDBytes()))

	query, args := db.Build()
	tag, delErr := s.exec.Exec(ctx, query, args...)
	if delErr != nil {
		return fmt.Errorf("signup store: delete: %w", delErr)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// PeekValid resolves a token by hash; exhausted/expired fail.
func (s *PostgresStore) PeekValid(ctx context.Context, rawHash string) (*SignupToken, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(tokenColumns...)
	sb.From(signupTokensTable + " t")
	sb.Where(sb.And(
		sb.E("t.token_hash", rawHash),
		sb.GT("t.expires_at", time.Now().UTC()),
		"t.usage_count < t.usage_limit",
	))

	query, args := sb.Build()
	row, err := scanToken(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("signup store: peek: %w", err)
	}
	return row, nil
}

// ConsumeUse increments usage atomically; exhausted tokens fail.
func (s *PostgresStore) ConsumeUse(ctx context.Context, id SignupTokenID) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(signupTokensTable)
	ub.Set(ub.Assign("usage_count", sqlbuilder.Buildf("usage_count + 1")))
	ub.Where(ub.And(
		ub.E("id", id.UUIDBytes()),
		"usage_count < usage_limit",
	))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("signup store: consume: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrExhausted
	}
	return nil
}

// scanToken scans one row; keep in sync with tokenColumns.
func scanToken(row scanner) (*SignupToken, error) {
	var (
		id      string
		t       SignupToken
		created pgtype.Timestamptz
		expires pgtype.Timestamptz
	)
	if err := row.Scan(&id, &t.TokenHash, &t.UsageLimit, &t.UsageCount, &created, &expires); err != nil {
		return nil, err
	}

	parsed, err := typeid.FromUUID[SignupTokenID](id)
	if err != nil {
		return nil, fmt.Errorf("signup store: token id %q is not a UUID: %w", id, err)
	}
	t.ID = parsed
	t.CreatedAt = created.Time
	t.ExpiresAt = expires.Time
	return &t, nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}
