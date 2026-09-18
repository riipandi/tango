// Package session manages sign-in sessions: issue (cookie-backed
// opaque tokens), validate (sliding expiry), list, and revoke. The
// sign-in endpoint lives here; credential checking is delegated to
// a Verifier (the password feature) via consumer-side wiring.
package session

import (
	"context"
	"errors"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity/user"
)

// Typed IDs for the session tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	sessionPrefix struct{}

	SessionID = typeid.TypeID[sessionPrefix]

	authTokenPrefix struct{}

	AuthTokenID = typeid.TypeID[authTokenPrefix]

	refreshTokenPrefix struct{}

	RefreshTokenID = typeid.TypeID[refreshTokenPrefix]
)

func (sessionPrefix) Prefix() string      { return "session" }
func (authTokenPrefix) Prefix() string    { return "auth_token" }
func (refreshTokenPrefix) Prefix() string { return "refresh_token" }

// Session is one active sign-in. The ID is public (listed, revoked
// by users); the token never leaves the service — only its SHA-256
// hash is stored, and only the cookie carries it.
type Session struct {
	ID        string
	UserID    user.UserID
	Provider  string
	TokenHash string

	UserAgent   *string
	DeviceName  *string
	IPAddress   *string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RefreshedAt *time.Time
	RevokedAt   *time.Time
}

// Meta carries request context captured at sign-in.
type Meta struct {
	UserAgent  string
	DeviceName string
	IPAddress  string
}

// Errors surfaced by stores and the service.
var (
	// ErrNotFound is returned when no session matches, including
	// expired and revoked ones (they must be indistinguishable).
	ErrNotFound = errors.New("session: not found")
)

// Store persists sessions. Validity (revoked/expired) is enforced
// by every read.
type Store interface {
	Create(ctx context.Context, s *Session) error
	ValidByTokenHash(ctx context.Context, tokenHash string) (Session, user.User, error)
	// ValidByID resolves one live session with its user by session
	// ID — the access-token check anchors on it.
	ValidByID(ctx context.Context, id string) (Session, user.User, error)
	Touch(ctx context.Context, id string, expiresAt time.Time) error
	// Rotate replaces the session's refresh token hash and restarts
	// its sliding expiry; the previous token stops resolving.
	Rotate(ctx context.Context, id string, tokenHash string, expiresAt time.Time) error
	RevokeByTokenHash(ctx context.Context, tokenHash string) error
	RevokeForUser(ctx context.Context, userID user.UserID, sessionID string) error
	RevokeAllForUser(ctx context.Context, userID user.UserID, exceptID string) error
	ListActiveForUser(ctx context.Context, userID user.UserID) ([]Session, error)
}
