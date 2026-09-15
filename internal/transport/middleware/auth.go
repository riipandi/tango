package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
)

// Principal and Authenticator are the kernel contracts; the middleware
// implements Authenticator (session resolver) structurally.
type Principal = kernel.Principal

// Authenticator resolves a session cookie token to a principal;
// wire the verifier in the api-key phase.
type Authenticator = kernel.Authenticator

// APIKeyVerifier resolves an X-API-KEY header value to a principal;
// wired by the registry once the api key feature lands.
type APIKeyVerifier func(ctx context.Context, key string) (Principal, error)

type principalContextKey struct{}

// WithPrincipal attaches the principal to the context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, p)
}

// PrincipalFromContext returns the request principal; ok is false
// on unauthenticated routes.
func PrincipalFromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalContextKey{}).(Principal)
	return p, ok
}

// RequireAuth resolves the session cookie and rejects anonymous
// requests with 401. The cookie name comes from the feature owning
// it (session.CookieName), keeping this package module-free.
func RequireAuth(auth Authenticator, cookieName string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(cookieName)
			if err != nil || cookie.Value == "" {
				responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
				return
			}

			principal, err := auth.ResolveSession(r.Context(), cookie.Value)
			if err != nil {
				responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired session")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}

// RequireAdmin rejects non-admin principals with 403; run it after
// RequireAuth.
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

// RequireAPIKey resolves the X-API-KEY header for machine clients;
// wire the verifier in the api-key phase.
func RequireAPIKey(verifier APIKeyVerifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := strings.TrimSpace(r.Header.Get("X-API-KEY"))
			if key == "" {
				responder.Fail(w, r, http.StatusUnauthorized, "API key required")
				return
			}

			principal, err := verifier(r.Context(), key)
			if err != nil {
				responder.Fail(w, r, http.StatusUnauthorized, "invalid API key")
				return
			}

			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), principal)))
		})
	}
}
