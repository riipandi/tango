package middleware

import (
	"net/http"
	"strings"

	jsonv2 "encoding/json/v2"
)

// BearerAuth enforces the first-party RPC authentication contract:
// protected Connect methods authenticate with
// `Authorization: Bearer <internal-access-token>`. Cookie presence
// alone never authorizes an RPC. Public RPC services are mounted
// without this guard.
func BearerAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r); !ok {
			writeConnectError(w, "unauthenticated", "bearer token required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RPCSessionAuth authenticates a protected Connect service from the
// bearer header only: the token is the internal session access token
// and resolves through the kernel authenticator. Cookies are token
// storage, never an RPC fallback. The resolved principal lands in the
// request context; anonymous or invalid tokens answer the Connect
// unauthenticated error.
func RPCSessionAuth(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := bearerToken(r)
			if !ok {
				writeConnectError(w, "unauthenticated", "bearer token required")
				return
			}
			principal, err := auth.ResolveSession(r.Context(), token)
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

// bearerToken extracts the credentials from Authorization: Bearer.
func bearerToken(r *http.Request) (string, bool) {
	auth := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(auth, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return "", false
	}
	return token, true
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
