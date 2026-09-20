package middleware

import (
	"context"
	"net/http"
	"time"
)

// RequestTimeout bounds every request with a hard context deadline.
// Handlers surface the deadline themselves (Connect maps it to
// DeadlineExceeded); the middleware never writes its own response.
func RequestTimeout(budget time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), budget)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
