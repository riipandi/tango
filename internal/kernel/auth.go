package kernel

import (
	"context"
	"net/http"
)

// Guard wraps a handler chain.
type Guard func(http.Handler) http.Handler

// Guarded marks routes that require an injected guard.
type Guarded interface {
	UseGuard(guard Guard)
}

// Principal is the authenticated actor attached to a request.
type Principal struct {
	SessionID string
	UserID    string
	Username  string
	Email     string
	Provider  string
	IsAdmin   bool
}

// Authenticator resolves a session token to a principal.
type Authenticator interface {
	ResolveSession(ctx context.Context, token string) (Principal, error)
}
