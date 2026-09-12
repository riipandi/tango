package user

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
)

// PostgresStore persists users in public.users. It builds on the
// shared Executor (pool or transaction) — never owns connections.
// Queries are composed with go-sqlbuilder (PostgreSQL flavor).
type PostgresStore struct {
	exec datastore.Executor
}

var _ Store = (*PostgresStore)(nil)

// NewPostgresStore builds the production user store.
func NewPostgresStore(exec datastore.Executor) *PostgresStore {
	return &PostgresStore{exec: exec}
}

// userColumns is the SELECT list shared by every query; keep the
// order in sync with scanUser.
var userColumns = []string{
	"id", "username", "email", "first_name", "last_name",
	"display_name", "avatar_url", "locale", "is_admin", "disabled",
	"email_verified_at", "created_at", "updated_at", "last_login_at",
}

// List returns every user, newest first. Unreadable rows are
// skipped; a failed query yields an empty slice.
func (s *PostgresStore) List(ctx context.Context) []User {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(userColumns...)
	sb.From(usersTable)
	sb.OrderBy("created_at DESC", "id DESC")

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return []User{}
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		user, scanErr := scanUser(rows)
		if scanErr != nil {
			continue
		}
		users = append(users, user)
	}
	return users
}

// Create inserts a user and returns the stored row. Optional names
// store as NULL (nil pointers); the display name falls back to the
// email local part before the insert.
func (s *PostgresStore) Create(ctx context.Context, params CreateParams) (User, error) {
	displayName := params.DisplayName
	if displayName == "" {
		local, _, _ := strings.Cut(params.Email, "@")
		displayName = local
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto(usersTable)
	ib.Cols("username", "email", "first_name", "last_name", "display_name", "is_admin")
	ib.Values(
		params.Username,
		params.Email,
		textOrNull(params.FirstName),
		textOrNull(params.LastName),
		displayName,
		params.IsAdmin,
	)
	ib.Returning("id", "first_name", "last_name", "created_at")

	query, args := ib.Build()

	var (
		id        string
		firstName pgtype.Text
		lastName  pgtype.Text
		createdAt pgtype.Timestamptz
	)
	err := s.exec.QueryRow(ctx, query, args...).Scan(&id, &firstName, &lastName, &createdAt)
	if err != nil {
		return User{}, mapStoreError(err)
	}

	return User{
		ID:          mustUserID(id),
		Username:    params.Username,
		Email:       params.Email,
		FirstName:   textPtr(firstName),
		LastName:    textPtr(lastName),
		DisplayName: displayName,
		IsAdmin:     params.IsAdmin,
		CreatedAt:   createdAt.Time,
	}, nil
}

// GetByID resolves one user; unknown IDs surface ErrNotFound.
func (s *PostgresStore) GetByID(ctx context.Context, id UserID) (User, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(userColumns...)
	sb.From(usersTable)
	sb.Where(sb.E("id", id.UUIDBytes()))

	query, args := sb.Build()
	user, err := scanUser(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

// scanner covers pgx.Rows and pgx.Row.
type scanner interface {
	Scan(dest ...any) error
}

// scanUser scans one row into the domain type; keep the column
// order in sync with userColumns.
func scanUser(row scanner) (User, error) {
	var (
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
	err := row.Scan(&id, &username, &email, &firstName, &lastName, &displayName,
		&avatarURL, &locale, &isAdmin, &disabled,
		&emailVerifiedAt, &createdAt, &updatedAt, &lastLoginAt)
	if err != nil {
		return User{}, mapStoreError(err)
	}

	return User{
		ID:              mustUserID(id),
		Username:        username,
		Email:           email,
		FirstName:       textPtr(firstName),
		LastName:        textPtr(lastName),
		AvatarURL:       textPtr(avatarURL),
		Locale:          textPtr(locale),
		DisplayName:     displayName,
		IsAdmin:         isAdmin,
		Disabled:        disabled,
		EmailVerifiedAt: timePtr(emailVerifiedAt),
		CreatedAt:       createdAt.Time,
		UpdatedAt:       timePtr(updatedAt),
		LastLoginAt:     timePtr(lastLoginAt),
	}, nil
}

func textPtr(t pgtype.Text) *string {
	if !t.Valid {
		return nil
	}
	return &t.String
}

// textOrNull maps empty strings to SQL NULL.
func textOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// mustUserID converts a stored UUID to the typed ID. TypeIDs wrap
// UUIDs losslessly, so failure is a programmer error.
func mustUserID(uuidText string) UserID {
	id, err := typeid.FromUUID[UserID](uuidText)
	if err != nil {
		panic(fmt.Sprintf("user: stored id %q is not a UUID: %v", uuidText, err))
	}
	return id
}

// mapStoreError translates driver errors into the Store contract.
func mapStoreError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23505": // unique_violation
			return ErrDuplicate
		case "23514": // check_violation — DB constraint names end in _check
			if strings.Contains(pgErr.ConstraintName, "email") {
				return ErrInvalidEmail
			}
			if strings.Contains(pgErr.ConstraintName, "username") {
				return ErrInvalidUsername
			}
		}
	}
	return fmt.Errorf("user store: %w", err)
}
