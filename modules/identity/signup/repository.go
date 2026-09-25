package signup

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgconn"
	"uuid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
)

// Repository writes the account a sign-up creates and consumes the token it
// was created under. Every method takes the query surface, so the service
// passes either the pool or the transaction it runs in.
type Repository struct{}

// NewRepository builds the repository over the shared pool.
func NewRepository() *Repository {
	return &Repository{}
}

// FindSignupTokenByHash reads the token a raw value hashes to. The raw value
// is never stored: only the caller's hash reaches this query.
func (r *Repository) FindSignupTokenByHash(ctx context.Context, db datastore.Querier, tokenHash string) (*SignupToken, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "usage_limit", "usage_count", "created_at", "expires_at")
	sb.From(SignupTokenTable)
	sb.Where(sb.Equal("token_hash", tokenHash))

	query, args := sb.Build()
	var row SignupToken
	err := db.QueryRow(ctx, query, args...).Scan(&row.ID, &row.UsageLimit, &row.UsageCount, &row.CreatedAt, &row.ExpiresAt)
	if errors.Is(err, datastore.ErrNoRows) {
		return nil, datastore.ErrNoRows
	}
	if err != nil {
		return nil, fmt.Errorf("signup: find token: %w", err)
	}
	return &row, nil
}

// CreateUser inserts the account row and returns its identifier.
func (r *Repository) CreateUser(ctx context.Context, db datastore.Querier, row user.UserSchema) (uuid.UUID, error) {
	row.ID = uuid.NewV7()

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(user.UserTable)
	ib.Cols("id", "username", "email", "first_name", "last_name", "display_name")
	// The name columns are nullable and an absent name is NULL, not the
	// empty string the struct's zero value carries.
	ib.Values(row.ID, row.Username, row.Email, nullIfEmpty(row.FirstName), nullIfEmpty(row.LastName), row.DisplayName)

	query, args := ib.Build()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return uuid.Nil(), err
	}
	return row.ID, nil
}

// nullIfEmpty turns an absent optional field into the SQL NULL its column
// stores.
func nullIfEmpty(value string) any {
	if value == "" {
		return nil
	}
	return value
}

// CreatePassword stores the account's primary credential.
func (r *Repository) CreatePassword(ctx context.Context, db datastore.Querier, row password.UserPasswordSchema) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(password.UserPasswordTable)
	ib.Cols("user_id", "password_hash")
	ib.Values(row.UserID, row.PasswordHash)

	query, args := ib.Build()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return err
	}
	return nil
}

// ConsumeSignupToken counts one use against the token. The conditional update
// is the whole single-use rule: a token that is spent or expired between the
// read and this statement bumps no row, and the caller refuses the signup.
func (r *Repository) ConsumeSignupToken(ctx context.Context, db datastore.Querier, tokenID uuid.UUID, at time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(SignupTokenTable)
	ub.Set(ub.Assign("usage_count", sqlbuilder.Raw("usage_count + 1")))
	ub.Where(
		ub.Equal("id", tokenID),
		ub.LessThan("usage_count", sqlbuilder.Raw("usage_limit")),
		ub.GreaterThan("expires_at", at),
	)

	query, args := ub.Build()
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("signup: consume token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrInvalidToken
	}
	return nil
}

// errUniqueViolation reports whether the insert failed on a unique index, the
// way the username and email indexes answer a duplicate account.
func errUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// CreateSignupToken inserts the hashed row and answers its identifier. The
// raw value is the caller's to show once; only the hash reaches this table.
func (r *Repository) CreateSignupToken(ctx context.Context, db datastore.Querier, tokenHash string, usageLimit int32, expiresAt time.Time) (uuid.UUID, error) {
	id := uuid.NewV7()

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(SignupTokenTable)
	ib.Cols("id", "token_hash", "usage_limit", "expires_at")
	ib.Values(id, tokenHash, usageLimit, expiresAt)

	query, args := ib.Build()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return uuid.Nil(), fmt.Errorf("signup: create token: %w", err)
	}
	return id, nil
}

// ListSignupTokens answers one page of the issued tokens, newest first, with
// the total count the pagination metadata needs.
func (r *Repository) ListSignupTokens(ctx context.Context, db datastore.Querier, offset, limit int) ([]SignupToken, int, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "usage_limit", "usage_count", "created_at", "expires_at")
	sb.From(SignupTokenTable)
	sb.OrderBy("created_at DESC", "id DESC")
	sb.Limit(limit).Offset(offset)

	query, args := sb.Build()
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("signup: list tokens: %w", err)
	}
	defer rows.Close()

	tokens := []SignupToken{}
	for rows.Next() {
		var row SignupToken
		if err := rows.Scan(&row.ID, &row.UsageLimit, &row.UsageCount, &row.CreatedAt, &row.ExpiresAt); err != nil {
			return nil, 0, fmt.Errorf("signup: list tokens: %w", err)
		}
		tokens = append(tokens, row)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("signup: list tokens: %w", err)
	}

	cb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	cb.Select("count(*)")
	cb.From(SignupTokenTable)
	query, args = cb.Build()
	var total int
	if err := db.QueryRow(ctx, query, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("signup: count tokens: %w", err)
	}
	return tokens, total, nil
}

// DeleteSignupToken removes an issued token. It answers whether a row was
// removed, so the caller refuses an id that names nothing.
func (r *Repository) DeleteSignupToken(ctx context.Context, db datastore.Querier, id uuid.UUID) (bool, error) {
	db2 := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db2.DeleteFrom(SignupTokenTable)
	db2.Where(db2.Equal("id", id))

	query, args := db2.Build()
	tag, err := db.Exec(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("signup: delete token: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
