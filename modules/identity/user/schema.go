// Package user is the mandatory accounts core: user entities, their
// CRUD surface, and persistence. Every other feature hangs off user
// accounts; the composition root always passes it first to
// identity.New.
package user

import (
	"context"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/pkg/responder"
)

// usersTable is the table backing the User entity.
const usersTable = "public.users"

// Typed IDs for the user tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	userPrefix struct{}

	UserID = typeid.TypeID[userPrefix]

	userPhonePrefix struct{}

	UserPhoneID = typeid.TypeID[userPhonePrefix]
)

func (userPrefix) Prefix() string      { return "user" }
func (userPhonePrefix) Prefix() string { return "user_phone" }

// User is the core identity entity, shared by all features. Nullable
// columns use pointers; empty string is never stored for them.
type User struct {
	ID        UserID  `json:"id"`
	Username  string  `json:"username"`
	Email     string  `json:"email"`
	FirstName *string `json:"first_name,omitzero"`
	LastName  *string `json:"last_name,omitzero"`
	AvatarURL *string `json:"avatar_url,omitzero"`
	Locale    *string `json:"locale,omitzero"`

	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	Disabled    bool   `json:"disabled"`

	EmailVerifiedAt *time.Time `json:"email_verified_at,omitzero"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at,omitzero"`
	LastLoginAt     *time.Time `json:"last_login_at,omitzero"`

	// ProfilePicturePath is the blob-store path of the custom
	// picture (phase 9C); nil falls back to the bundled default.
	// Internal: never serialized (the .png route serves bytes).
	ProfilePicturePath *string `json:"-"`
}

// ListParams narrows and pages the admin listing.
type ListParams struct {
	Query string // matches username, email, display name (ILIKE)
	// PaginationParams pages the result set.
	responder.PaginationParams
}

// Store abstracts user persistence: the memory store backs tests,
// the Postgres store backs production. Implementations assign IDs
// and timestamps.
type Store interface {
	List(ctx context.Context, params ListParams) ([]User, int, error)
	Create(ctx context.Context, params CreateParams) (User, error)
	GetByID(ctx context.Context, id UserID) (User, error)
	UpdateAdmin(ctx context.Context, id UserID, params AdminUpdateParams) (User, error)
	UpdateProfile(ctx context.Context, id UserID, params UpdateProfileParams) (User, error)
	// SetProfilePicturePath stores or clears (nil) the blob path of
	// the user's profile picture (phase 9C).
	SetProfilePicturePath(ctx context.Context, id UserID, path *string) error
	MarkLogin(ctx context.Context, id UserID) error
	MarkEmailVerified(ctx context.Context, id UserID) error
	Delete(ctx context.Context, id UserID) error
}

// ImageStore is the consumer-side blob adapter (internal/storage
// backend): write, read, remove. Keeping it an interface stops the
// identity tree from importing internal/storage directly.
type ImageStore interface {
	Save(ctx context.Context, path string, data io.Reader) error
	Open(ctx context.Context, path string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, path string) error
}

// DefaultPictureFunc serves the bundled default profile picture
// (wired from the appimage module); ok=false when none is set.
type DefaultPictureFunc func(ctx context.Context) (reader io.ReadCloser, size int64, mime string, ok bool)

// AdminUpdateParams patches administrative fields. Nil pointers keep
// the current value.
type AdminUpdateParams struct {
	Email       *string
	FirstName   *string
	LastName    *string
	DisplayName *string
	IsAdmin     *bool
	Disabled    *bool
}

// UpdateProfileParams patches the profile fields. Nil pointers keep
// the current value; a non-nil pointer replaces the column (an empty
// string clears nullable columns; display name must stay non-empty).
type UpdateProfileParams struct {
	FirstName   *string
	LastName    *string
	DisplayName *string
	AvatarURL   *string
	Locale      *string
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

// UsernamePattern mirrors the database CHECK constraint; admin DTOs
// reuse it for validation.
var UsernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

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

	if !UsernamePattern.MatchString(p.Username) {
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
