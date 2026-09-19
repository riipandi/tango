package middleware

import (
	"context"
	"net/http"
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
)

// RPCSessionAuth authenticates a protected Connect service from the
// bearer header only: the token is the internal short-lived access
// JWT and resolves through the access authenticator, which
// re-checks session revocation. Cookies are token storage, never an
// RPC fallback. The resolved principal lands in the request
// context; anonymous or invalid tokens answer the Connect
// unauthenticated error.
func RPCSessionAuth(auth kernel.AccessAuthenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerFromHeader(r.Header)
			if !ok {
				writeConnectError(w, "unauthenticated", "bearer token required")
				return
			}
			principal, err := auth.ResolveAccess(r.Context(), token)
			if err != nil {
				// Enumeration-safe: expired, revoked, and unknown
				// tokens are indistinguishable.
				writeConnectError(w, "unauthenticated", "invalid or expired token")
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// BearerFromHeader extracts the credentials from an Authorization:
// Bearer header, for both middleware and Connect interceptors.
func BearerFromHeader(h http.Header) (string, bool) {
	scheme, token, found := strings.Cut(h.Get("Authorization"), " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
}

// ResolveBearer maps a Connect request's bearer header onto a
// principal via the access authenticator. Enumeration-safe: expired,
// revoked, and unknown tokens share one message.
func ResolveBearer(ctx context.Context, auth kernel.AccessAuthenticator, h http.Header) (kernel.Principal, error) {
	token, ok := BearerFromHeader(h)
	if !ok {
		return kernel.Principal{}, rpcerr.Unauthenticated("bearer token required")
	}
	principal, err := auth.ResolveAccess(ctx, token)
	if err != nil {
		return kernel.Principal{}, rpcerr.Unauthenticated("invalid or expired token")
	}
	return principal, nil
}

// writeConnectError answers with a Connect protocol error body so RPC
// clients decode a typed error instead of a transport-level status.
func writeConnectError(w http.ResponseWriter, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(connectStatus(code))
	_ = jsonv2.MarshalWrite(w, map[string]string{"code": code, "message": message})
}

// connectStatus maps the Connect codes the middleware can emit onto
// their protocol HTTP status.
func connectStatus(code string) int {
	switch code {
	case "unauthenticated":
		return http.StatusUnauthorized
	case "permission_denied":
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}
