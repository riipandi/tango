// Package user is the mandatory accounts core: user entities, their
// CRUD surface, and persistence. Every other feature hangs off user
// accounts; the composition root always passes it first to
// identity.New.
package user

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"github.com/riipandi/tango/modules/identity"
)

// Store abstracts user persistence: the memory store backs tests,
// the Postgres store backs production. Implementations assign IDs
// and timestamps.
type Store interface {
	List(ctx context.Context) []identity.User
	Create(ctx context.Context, params CreateParams) (identity.User, error)
	GetByID(ctx context.Context, id identity.UserID) (identity.User, error)
}

// Errors surfaced by stores and mapped to HTTP statuses by handlers.
var (
	// ErrNotFound is returned when no user matches the ID.
	ErrNotFound = errors.New("user not found")
	// ErrDuplicate is returned when username or email already exists.
	ErrDuplicate = errors.New("username or email already exists")
	// ErrInvalidUsername rejects usernames outside ^[a-zA-Z0-9_]{3,32}$.
	ErrInvalidUsername = errors.New("username must be 3-32 characters: letters, digits, underscores")
	// ErrInvalidEmail rejects emails missing a local part, domain, or TLD.
	ErrInvalidEmail = errors.New("email is not a valid address")
)

// usernamePattern mirrors the database CHECK constraint.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// emailPattern mirrors the database CHECK constraint.
var emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

// CreateParams carries the fields a caller supplies; the store fills
// the rest (ID, timestamps, defaults).
type CreateParams struct {
	Username    string
	Email       string
	FirstName   string
	LastName    string
	DisplayName string
	IsAdmin     bool
}

// Validate normalizes params and enforces the same rules as the
// database constraints, so invalid writes fail before reaching
// Postgres. Returns the display name resolved from first/last name
// or the email local part.
func (p CreateParams) Validate() (displayName string, err error) {
	p.Username = strings.ToLower(strings.TrimSpace(p.Username))
	p.Email = strings.TrimSpace(p.Email)
	p.FirstName = strings.TrimSpace(p.FirstName)
	p.LastName = strings.TrimSpace(p.LastName)

	if !usernamePattern.MatchString(p.Username) {
		return "", ErrInvalidUsername
	}
	if !emailPattern.MatchString(p.Email) {
		return "", ErrInvalidEmail
	}

	displayName = strings.TrimSpace(p.DisplayName)
	if displayName == "" {
		displayName = strings.TrimSpace(p.FirstName + " " + p.LastName)
	}
	if displayName == "" {
		if local, _, ok := strings.Cut(p.Email, "@"); ok {
			displayName = local
		}
	}
	return displayName, nil
}
