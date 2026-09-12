package middleware

import (
	"net/http"
	"strings"

	"github.com/riipandi/tango/pkg/responder"
)

// RequestIDHeader carries the correlation ID; clients may supply
// their own value (e.g. retries of the same logical operation).
const RequestIDHeader = "X-Request-Id"

// RequestID resolves one correlation ID per request: incoming header
// or a fresh TypeID (UUIDv7). The ID lands in the context (for
// loggers and responders) and in the response header.
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
