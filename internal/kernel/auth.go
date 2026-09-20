package kernel

import (
	"context"
	"net/http"
)

// Guard wraps a handler chain.
type Guard func(http.Handler) http.Handler

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

// AccessAuthenticator resolves an internal RPC bearer access token
// (the short-lived JWT) to a principal, re-checking session
// revocation so tokens cannot outlive their family.
type AccessAuthenticator interface {
	ResolveAccess(ctx context.Context, token string) (Principal, error)
}
