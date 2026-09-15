package middleware

import (
	"net/http"
	"strings"

	"github.com/riipandi/tango/pkg/responder"
)

// RequestIDHeader names the request correlation header.
const RequestIDHeader = "X-Request-Id"

// RequestID stores an incoming or generated ID in the context and response.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get(RequestIDHeader))
		if id == "" {
			id = responder.NewRequestID()
		}

		w.Header().Set(RequestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(responder.WithRequestID(r.Context(), id)))
	})
}
