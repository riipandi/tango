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

// purpose on auth_tokens for this module.
const purpose = "email_verification"

// Errors surfaced to handlers.
var (
	// ErrNotFound covers unknown/expired tokens without leaking.
	ErrNotFound = errors.New("emailverification: token is invalid or expired")
)

// Token is one auth_tokens row (purpose email_verification).
type Token struct {
	ID        string
	UserID    string
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// Store persists verification tokens.
type Store interface {
	// Upsert writes (or replaces) the single token per user+purpose.
	Upsert(ctx context.Context, token *Token) error
	// Consume deletes + returns the token for the hash; only an
	// unexpired token resolves.
	Consume(ctx context.Context, tokenHash string) (*Token, error)
}

// Verifier marks the user's email verified — implemented by the
// user store via the registry.
type Verifier interface {
	MarkEmailVerified(ctx context.Context, userID string) error
}
