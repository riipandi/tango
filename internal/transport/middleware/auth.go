package middleware

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
)

// Principal aliases the kernel principal type.
type Principal = kernel.Principal

// Authenticator resolves a session token to a principal.
type Authenticator = kernel.Authenticator

// APIKeyVerifier resolves an API key to a principal.
type APIKeyVerifier func(ctx context.Context, key string) (Principal, error)

type principalContextKey struct{}

// WithPrincipal stores a principal in the context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// PrincipalFromContext returns the request principal.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}

// RequireAuth resolves the session credential from the Authorization
// bearer header and rejects anonymous requests. The caller presents
// the session token the sign-in response returned; there is no cookie
// channel.
func RequireAuth(auth Authenticator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token, ok := BearerFromHeader(r.Header)
			if !ok {
				responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			principal, err := auth.ResolveSession(r.Context(), token)
			if err != nil {
				responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// RequireAdmin rejects non-admin principals.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
			return
		}
		if !principal.IsAdmin {
			responder.Fail(w, r, http.StatusForbidden, "administrator access required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
