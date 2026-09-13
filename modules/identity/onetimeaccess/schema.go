// Package onetimeaccess issues single-use sign-in tokens delivered
// by email: admins mint them per user; unauthenticated users may
// request one for their own address when the app policy allows.
// Storage: auth_tokens (purpose = one_time_access, hash at rest).
package onetimeaccess

import (
	"context"
	"errors"
	"time"
)

// Token lifetime + resend throttle — upstream parity.
const (
	// TokenTTL bounds one-time access tokens.
	TokenTTL = 15 * time.Minute
	// ResendThrottle bounds re-request frequency.
	ResendThrottle = time.Minute
)

// Errors surfaced to handlers.
var (
	// ErrNotFound covers unknown tokens/users without leaking which.
	ErrNotFound = errors.New("onetimeaccess: token is invalid or expired")
	// ErrThrottled rejects re-requests inside the resend window.
	ErrThrottled = errors.New("onetimeaccess: request throttled")
)

// purpose on auth_tokens for this module.
const purpose = "one_time_access"

// Token is one auth_tokens row (purpose one_time_access).
type Token struct {
	ID         string
	UserID     string
	TokenHash  string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSentAt *time.Time
}

// Store persists one-time access tokens.
type Store interface {
	// Upsert writes (or replaces) the single token per user+purpose
	// (unique index user_id+purpose) and reports whether the resend
	// throttle blocked it.
	Upsert(ctx context.Context, token *Token) (bool, error)
	// Consume deletes + returns the token row for the hash; only an
	// unexpired token resolves.
	Consume(ctx context.Context, tokenHash string) (*Token, error)
}
