// Package onetimeaccess issues single-use sign-in tokens delivered
// by email: admins mint them per user; unauthenticated users may
// request one for their own address when the app policy allows.
// Storage: auth_tokens (purpose = one_time_access, hash at rest)
// via the shared token store.
package onetimeaccess

import (
	"errors"
	"time"
)

// Token lifetime and resend throttle.
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
