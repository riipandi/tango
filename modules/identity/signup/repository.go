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
	sb.Select("id", "usage_limit", "usage_count", "expires_at")
	sb.From(SignupTokenTable)
	sb.Where(sb.Equal("token_hash", tokenHash))

	query, args := sb.Build()
	var row SignupToken
	err := db.QueryRow(ctx, query, args...).Scan(&row.ID, &row.UsageLimit, &row.UsageCount, &row.ExpiresAt)
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
