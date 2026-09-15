package kernel

import (
	"context"
	"net/http"
)

// Guard wraps a handler chain (route middleware). Modules type their
// guard options with it instead of re-declaring local aliases.
type Guard func(http.Handler) http.Handler

// Guarded is implemented by modules and features whose routes sit
// behind an injected guard. The composition root (registry) wires the
// production guard; a nil guard must fail closed (skip mounting).
type Guarded interface {
	UseGuard(guard Guard)
}

// Principal is the authenticated actor attached to a request.
// Transport-level strings; handlers parse typed IDs as needed.
type Principal struct {
	SessionID string
	UserID    string
	Username  string
	Email     string
	Provider  string
	IsAdmin   bool
}

// Authenticator resolves a session cookie token to a principal.
// Implemented by the session feature; declared here so identity
// services never import the transport layer.
type Authenticator interface {
	ResolveSession(ctx context.Context, token string) (Principal, error)
}
