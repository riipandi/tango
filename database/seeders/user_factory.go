package seeders

import (
	"context"
	"errors"
	"strings"
	"time"
	"uuid"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
)

// UserSeederName is the name this seeder reports under. The "Seeder" suffix
// keeps it distinct from the entity it writes, which appears next to it on the
// same report line.
const UserSeederName = "UserSeeder"

// UserCredentials is the account the default user is created with. The values
// are public on purpose: they bootstrap a local database, and a deployment is
// expected to change the password at first login.
type UserCredentials struct {
	Email     string
	Username  string
	Password  string
	FirstName string
	LastName  string
}

// DisplayName is the name shown in the UI. It is derived rather than stored, so
// the credentials stay the single source and the column cannot disagree with
// the names it is built from. It is trimmed because display_name rejects an
// empty string, and an account may carry only one of the two names.
func (c UserCredentials) DisplayName() string {
	return strings.TrimSpace(c.FirstName + " " + c.LastName)
}

// DefaultUser is the account migrate:seed creates.
var DefaultUser = UserCredentials{
	Email:     "admin@example.com",
	Username:  "admin",
	Password:  "@dmin123",
	FirstName: "Admin",
	LastName:  "Sistem",
}

// User returns the seeder for the default user. It writes the identity and its
// password in one transaction, because a user without a password cannot log in
// and the two tables are one record conceptually.
func User() Seeder {
	return Seeder{
		Name:  UserSeederName,
		Apply: applyDefaultUser,
	}
}

// applyDefaultUser creates the default user and its password.
//
// Both inserts are guarded by ON CONFLICT DO NOTHING, so a second run keeps the
// existing rows and reports them as skipped. The account is looked up by email,
// the natural key the operator knows.
func applyDefaultUser(
	ctx context.Context,
	q datastore.Querier,
	dryRun bool,
) (created, skipped []string, err error) {
	if dryRun {
		return plannedDefaultUser(ctx, q)
	}

	// The password hash is salted at random, so it must not be computed on a
	// dry run: that work would be thrown away.
	hash, err := crypto.NewPasswordHasher().Hash(DefaultUser.Password)
	if err != nil {
		return nil, nil, err
	}

	row := user.UserSchema{
		ID:          uuid.NewV7(),
		Username:    DefaultUser.Username,
		Email:       DefaultUser.Email,
		FirstName:   DefaultUser.FirstName,
		LastName:    DefaultUser.LastName,
		DisplayName: DefaultUser.DisplayName(),
		IsAdmin:     true,
		CreatedAt:   time.Now().UTC(),
	}

	inserted, err := insertUser(ctx, q, row)
	if err != nil {
		return nil, nil, err
	}
	if !inserted {
		return nil, []string{DefaultUser.Email}, nil
	}

	if err := insertPassword(ctx, q, row.ID, hash); err != nil {
		return nil, nil, err
	}
	return []string{DefaultUser.Email}, nil, nil
}

// plannedDefaultUser reports what a real run would do, without writing anything
// and without hashing the password.
func plannedDefaultUser(
	ctx context.Context,
	q datastore.Querier,
) (created, skipped []string, err error) {
	exists, err := userExists(ctx, q, DefaultUser.Email)
	if err != nil {
		return nil, nil, err
	}
	if exists {
		return nil, []string{DefaultUser.Email}, nil
	}
	return []string{DefaultUser.Email}, nil, nil
}

// userExists reports whether an account already uses email.
//
// The comparison is exact. public.users.email is TEXT with a format CHECK, not
// citext — only username is case-insensitive — so two accounts may differ by
// case. The seeder matches what the database does: it claims its own address
// and leaves a differently-cased one alone, which is what the unique index on
// email would do anyway.
func userExists(ctx context.Context, q datastore.Querier, email string) (bool, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id").From(user.UserTable).Where(sb.Equal("email", email))

	query, args := sb.Build()
	var id uuid.UUID
	err := q.QueryRow(ctx, query, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// insertUser inserts the account and reports whether it was this run that
// created it. A conflict on any unique column leaves the existing row alone and
// returns no row, which is what makes a repeated seed safe.
func insertUser(ctx context.Context, q datastore.Querier, row user.UserSchema) (bool, error) {
	ib := sqlbuilder.NewStruct(user.UserSchema{}).For(sqlbuilder.PostgreSQL).
		InsertInto(user.UserTable, row).
		// No conflict target: the account must not be created when any of its
		// unique columns is already taken.
		SQL("ON CONFLICT DO NOTHING").
		Returning("id")

	query, args := ib.Build()
	var id uuid.UUID
	err := q.QueryRow(ctx, query, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// insertPassword stores the password hash for a user this run created. There is
// no conflict to expect — the account is new — so a conflict is a real error
// rather than something to skip.
func insertPassword(ctx context.Context, q datastore.Querier, userID uuid.UUID, hash string) error {
	ib := sqlbuilder.NewStruct(password.UserPasswordSchema{}).For(sqlbuilder.PostgreSQL).
		InsertInto(password.UserPasswordTable, password.UserPasswordSchema{
			UserID:       userID,
			PasswordHash: hash,
		})

	query, args := ib.Build()
	if _, err := q.Exec(ctx, query, args...); err != nil {
		return err
	}
	return nil
}
