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
		if !hasBearerToken(r) {
			writeConnectUnauthenticated(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func hasBearerToken(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	scheme, token, found := strings.Cut(auth, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return false
	}
	return strings.TrimSpace(token) != ""
}

// writeConnectUnauthenticated answers with the Connect protocol
// unauthenticated error body so RPC clients decode a typed error
// instead of a transport-level 401.
func writeConnectUnauthenticated(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_ = jsonv2.MarshalWrite(w, map[string]string{
		"code":    "unauthenticated",
		"message": "bearer token required",
	})
}
