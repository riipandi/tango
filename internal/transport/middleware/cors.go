package middleware

import (
	"net/http"
	"time"

	corslib "github.com/rs/cors"

	"github.com/riipandi/tango/internal/config"
)

// CORS returns the cross-origin middleware the configuration asks for. The
// policy is applied to every route, so a preflight is answered wherever it
// lands and a deployment cannot forget one route's policy.
//
// An empty origin list keeps the policy closed: the middleware passes the
// request through without cross-origin headers, which is what a same-origin
// SPA needs. The middleware itself is disabled when no origin is named, rather
// than wrapping every call in a check it can never pass.
func CORS(cfg config.CORS) func(http.Handler) http.Handler {
	if len(cfg.AllowedOrigins) == 0 {
		return func(next http.Handler) http.Handler { return next }
	}

	handle := corslib.New(corslib.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   cfg.AllowedMethods,
		AllowedHeaders:   cfg.AllowedHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           int(cfg.MaxAge / time.Second),
	}).Handler

	return handle
}
