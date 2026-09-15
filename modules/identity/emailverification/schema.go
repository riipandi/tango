// Package emailverification verifies a user's email address via a
// single-use token (auth_tokens, purpose = email_verification).
// Send rides the mailer; the queue-backed async delivery lands in
// phase 7.
package emailverification

import (
	"context"
	"errors"
	"time"
)

// TokenTTL bounds verification tokens.
const TokenTTL = 24 * time.Hour

// Errors surfaced to handlers.
var (
	// ErrNotFound covers unknown/expired tokens without leaking.
	ErrNotFound = errors.New("emailverification: token is invalid or expired")
)

// Verifier marks the user's email verified — implemented by the
// user store via the registry.
type Verifier interface {
	MarkEmailVerified(ctx context.Context, userID string) error
}
