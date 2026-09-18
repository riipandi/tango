// Package token owns the shared one-shot token store
// (public.auth_tokens): rows keyed by (user_id, purpose), SHA-256
// hash at rest, single-use consumption with an expiry check.
// Consumers (emailverification, onetimeaccess) scope a store with
// their purpose and keep their own sentinels/TTLs.
package token

import (
	"context"
	"errors"
	"time"
)

// Purpose discriminates the auth_tokens rows; values are stored
// verbatim (DB CHECK constrains them).
type Purpose string

const (
	// PurposeEmailVerification covers email verification links.
	PurposeEmailVerification Purpose = "email_verification"
	// PurposeOneTimeAccess covers one-time access sign-in tokens.
	PurposeOneTimeAccess Purpose = "one_time_access"
	// PurposeReauthentication exists in the DB CHECK but has no consumer yet.
	PurposeReauthentication Purpose = "reauthentication"
	// PurposePasswordReset covers forgot/reset password tokens.
	PurposePasswordReset Purpose = "password_reset"
)

// ErrNotFound is returned by Consume for unknown, expired, or
// already-used tokens — indistinguishable by design. Consumers map
// it onto their own sentinel.
var ErrNotFound = errors.New("token: invalid or expired")

// Token is one auth_tokens row. UserID is the bare UUID column form.
type Token struct {
	ID        string
	UserID    string
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	// LastSentAt tracks the most recent delivery for resend
	// throttling; nil when never sent.
	LastSentAt *time.Time
}

// Store persists purpose-scoped tokens.
type Store interface {
	// Upsert writes (or replaces) the single token per user+purpose.
	Upsert(ctx context.Context, token *Token) error
	// Consume deletes + returns the token for the hash; only an
	// unexpired token resolves.
	Consume(ctx context.Context, tokenHash string) (*Token, error)
}

// Hash derives the at-rest form of a raw token (SHA-256, base64url).
func Hash(raw string) string {
	return hashToken(raw)
}
