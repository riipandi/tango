package password

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
)

// ErrWeakPassword rejects new passwords shorter than the minimum.
var ErrWeakPassword = responder.NewError(http.StatusUnprocessableEntity, "password: at least 8 characters required")

// ErrInvalidCredentials is returned for unknown identities and wrong
// secrets alike, so responses never reveal which one failed.
var ErrInvalidCredentials = responder.NewError(http.StatusBadRequest, "password: invalid credentials")

// ErrNoPassword is returned when the account has no credential row
// (e.g. passkey-only accounts).
var ErrNoPassword = errors.New("password: account has no password credential")

// Store persists credential hashes. One row per user (user_id PK).
type Store interface {
	Upsert(ctx context.Context, userID user.UserID, passwordHash string) error
	HashByIdentity(ctx context.Context, identity string) (string, user.User, error)
	HashByUserID(ctx context.Context, userID user.UserID) (string, error)
}

// PostgresStore persists credentials in public.user_passwords.
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production credential store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// Upsert replaces the credential row for the user.
func (s *PostgresStore) Upsert(ctx context.Context, userID user.UserID, passwordHash string) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(userPasswordsTable)
	ib.Cols("user_id", "password_hash")
	ib.Values(userID.UUIDBytes(), passwordHash)
	ib.SQL("ON CONFLICT (user_id) DO UPDATE SET")
	ib.SQL("password_hash = EXCLUDED.password_hash, updated_at = CURRENT_TIMESTAMP")

	query, args := ib.Build()
	if _, err := s.exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("password upsert: %w", err)
	}
	return nil
}

// HashByIdentity resolves a username or email to the credential hash
// and the owning user row, for sign-in verification.
func (s *PostgresStore) HashByIdentity(ctx context.Context, identity string) (string, user.User, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("p.password_hash",
		"u.id", "u.username", "u.email", "u.first_name", "u.last_name",
		"u.display_name", "u.avatar_url", "u.locale", "u.is_admin", "u.disabled",
		"u.email_verified_at", "u.created_at", "u.updated_at", "u.last_login_at")
	sb.From(userPasswordsTable + " p")
	sb.Join("public.users u ON u.id = p.user_id")
	sb.Where(sb.Or(sb.E("u.username", identity), sb.E("u.email", identity)))

	query, args := sb.Build()

	var (
		hash            string
		id              string
		username        string
		email           string
		firstName       pgtype.Text
		lastName        pgtype.Text
		displayName     string
		avatarURL       pgtype.Text
		locale          pgtype.Text
		isAdmin         bool
		disabled        bool
		emailVerifiedAt pgtype.Timestamptz
		createdAt       pgtype.Timestamptz
		updatedAt       pgtype.Timestamptz
		lastLoginAt     pgtype.Timestamptz
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(&hash, &id, &username, &email,
		&firstName, &lastName, &displayName, &avatarURL, &locale, &isAdmin, &disabled,
		&emailVerifiedAt, &createdAt, &updatedAt, &lastLoginAt)
	if err != nil {
		return "", user.User{}, ErrInvalidCredentials
	}

	u := user.User{
		ID:              user.MustID(id),
		Username:        username,
		Email:           email,
		FirstName:       datastore.TextPtr(firstName),
		LastName:        datastore.TextPtr(lastName),
		AvatarURL:       datastore.TextPtr(avatarURL),
		Locale:          datastore.TextPtr(locale),
		DisplayName:     displayName,
		IsAdmin:         isAdmin,
		Disabled:        disabled,
		EmailVerifiedAt: datastore.TimePtr(emailVerifiedAt),
		CreatedAt:       createdAt.Time,
		UpdatedAt:       datastore.TimePtr(updatedAt),
		LastLoginAt:     datastore.TimePtr(lastLoginAt),
	}
	return hash, u, nil
}

// HashByUserID returns the credential hash for a known user.
func (s *PostgresStore) HashByUserID(ctx context.Context, userID user.UserID) (string, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("password_hash")
	sb.From(userPasswordsTable)
	sb.Where(sb.E("user_id", userID.UUIDBytes()))

	query, args := sb.Build()
	var hash string
	if err := s.exec.QueryRow(ctx, query, args...).Scan(&hash); err != nil {
		return "", ErrNoPassword
	}
	return hash, nil
}
