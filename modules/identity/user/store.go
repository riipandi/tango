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
	"profile_picture_path",
}

// List returns matching users newest first plus the total count.
// Query matches username, email, or display name (case-insensitive).
func (s *PostgresStore) List(ctx context.Context, params ListParams) ([]User, int, error) {
	csb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	csb.Select("count(*)")
	csb.From(usersTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		csb.Where(csb.Or(csb.Like("username", pattern), csb.Like("email", pattern), csb.Like("display_name", pattern)))
	}

	countQuery, countArgs := csb.Build()
	var total int
	if err := s.exec.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("user store: count: %w", err)
	}

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select(userColumns...)
	sb.From(usersTable)
	if params.Query != "" {
		pattern := "%" + params.Query + "%"
		sb.Where(sb.Or(sb.Like("username", pattern), sb.Like("email", pattern), sb.Like("display_name", pattern)))
	}
	sb.OrderBy("created_at DESC", "id DESC")
	if !params.All() && params.Limit > 0 {
		sb.Limit(params.Limit).Offset(params.Offset())
	}

	query, args := sb.Build()
	rows, err := s.exec.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("user store: list: %w", err)
	}
	defer rows.Close()

	users := []User{}
	for rows.Next() {
		u, scanErr := scanUser(rows)
		if scanErr != nil {
			continue
		}
		users = append(users, u)
	}
	return users, total, rows.Err()
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
		ID:          MustID(id),
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

// UpdateAdmin patches administrative fields (email, names, flags)
// and returns the fresh row.
func (s *PostgresStore) UpdateAdmin(ctx context.Context, id UserID, params AdminUpdateParams) (User, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()

	var assignments []string
	apply := func(column string, value any) {
		assignments = append(assignments, ub.Assign(column, value))
	}
	if params.Email != nil {
		apply("email", *params.Email)
	}
	if params.FirstName != nil {
		apply("first_name", textOrNull(*params.FirstName))
	}
	if params.LastName != nil {
		apply("last_name", textOrNull(*params.LastName))
	}
	if params.DisplayName != nil {
		apply("display_name", *params.DisplayName)
	}
	if params.IsAdmin != nil {
		apply("is_admin", *params.IsAdmin)
	}
	if params.Disabled != nil {
		apply("disabled", *params.Disabled)
	}

	if len(assignments) == 0 {
		return s.GetByID(ctx, id)
	}

	ub.Update(usersTable)
	ub.Set(assignments...)
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.Returning(userColumns...)

	query, args := ub.Build()
	u, err := scanUser(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return u, nil
}

// Delete removes the user row; the database fn_soft_delete trigger
// archives the row into public.deleted_records. Unknown IDs surface
// ErrNotFound.
func (s *PostgresStore) Delete(ctx context.Context, id UserID) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom(usersTable)
	db.Where(db.E("id", id.UUIDBytes()))

	query, args := db.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("user store: delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// UpdateProfile patches profile columns and returns the fresh row.
// Nil params keep their column; named fields stay constraint-checked
// by the database (display_name > 0).
func (s *PostgresStore) UpdateProfile(ctx context.Context, id UserID, params UpdateProfileParams) (User, error) {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()

	var assignments []string
	apply := func(column string, value any) {
		assignments = append(assignments, ub.Assign(column, value))
	}
	if params.FirstName != nil {
		apply("first_name", textOrNull(*params.FirstName))
	}
	if params.LastName != nil {
		apply("last_name", textOrNull(*params.LastName))
	}
	if params.DisplayName != nil {
		apply("display_name", *params.DisplayName)
	}
	if params.AvatarURL != nil {
		apply("avatar_url", textOrNull(*params.AvatarURL))
	}
	if params.Locale != nil {
		apply("locale", textOrNull(*params.Locale))
	}

	if len(assignments) == 0 {
		return s.GetByID(ctx, id)
	}

	ub.Update(usersTable)
	ub.Set(assignments...)
	ub.Where(ub.E("id", id.UUIDBytes()))
	ub.Returning(userColumns...)

	query, args := ub.Build()
	user, err := scanUser(s.exec.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, err
	}
	return user, nil
}

// SetProfilePicturePath stores or clears (nil) the picture blob path.
func (s *PostgresStore) SetProfilePicturePath(ctx context.Context, id UserID, picturePath *string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(usersTable)
	ub.Set(ub.Assign("profile_picture_path", textOrNull(derefText(picturePath))))
	ub.Where(ub.E("id", id.UUIDBytes()))

	query, args := ub.Build()
	tag, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("set profile picture: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MarkLogin records a successful sign-in timestamp.
func (s *PostgresStore) MarkLogin(ctx context.Context, id UserID) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(usersTable)
	ub.Set(ub.Assign("last_login_at", time.Now().UTC()))
	ub.Where(ub.E("id", id.UUIDBytes()))

	query, args := ub.Build()
	_, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark login: %w", err)
	}
	return nil
}

// MarkEmailVerified stamps the email-verified timestamp.
func (s *PostgresStore) MarkEmailVerified(ctx context.Context, id UserID) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update(usersTable)
	ub.Set(ub.Assign("email_verified_at", time.Now().UTC()))
	ub.Where(ub.E("id", id.UUIDBytes()))

	query, args := ub.Build()
	_, err := s.exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("mark email verified: %w", err)
	}
	return nil
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
		picturePath     pgtype.Text
	)
	err := row.Scan(&id, &username, &email, &firstName, &lastName, &displayName,
		&avatarURL, &locale, &isAdmin, &disabled,
		&emailVerifiedAt, &createdAt, &updatedAt, &lastLoginAt, &picturePath)
	if err != nil {
		return User{}, mapStoreError(err)
	}

	return User{
		ID:              MustID(id),
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
		ProfilePicturePath: textPtr(picturePath),
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

// derefText flattens an optional string (nil → "" → NULL).
func derefText(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	return &t.Time
}

// MustID converts a stored UUID to the typed ID. TypeIDs wrap
// UUIDs losslessly, so failure is a programmer error. Exported for
// stores that join the users table.
func MustID(uuidText string) UserID {
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
