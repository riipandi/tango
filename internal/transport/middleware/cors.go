package middleware

import (
	"net/http"

	"github.com/go-chi/cors"
)

// CORS returns the cross-origin policy for the API and RPC surfaces.
// The header list mirrors what a first-party client actually sends:
// @connectrpc/connect sends Connect-Protocol-Version, Connect-Timeout-Ms,
// Connect-Accept-Encoding, and Connect-Content-Encoding on the Connect
// protocol, X-Grpc-Web and X-User-Agent on the gRPC-Web transport, plus
// Content-Type everywhere; the harness adds Authorization and X-API-KEY.
// Credentials stay disabled: RPCs authenticate with a bearer header or a
// machine key, never with a cookie, so a wildcard origin is safe.
func CORS() func(http.Handler) http.Handler {
	return cors.Handler(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Authorization",
			"Connect-Accept-Encoding",
			"Connect-Content-Encoding",
			"Connect-Protocol-Version",
			"Connect-Timeout-Ms",
			"Content-Encoding",
			"Content-Type",
			"X-API-KEY",
			"X-Grpc-Web",
			"X-User-Agent",
		},
		AllowCredentials: false,
		MaxAge:           300,
	})
}
