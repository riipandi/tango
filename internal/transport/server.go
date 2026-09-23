// Package transport serves the application over HTTP: the HTTP and ConnectRPC
// routers that mount every surface, the handler behind each route, and the
// server that binds them.
//
// The package is split by role, so a reader looking for one thing opens one
// file:
//
//   - http.go registers the HTTP surface: the request pipeline and the mounts
//     it composes.
//   - rpc.go registers the ConnectRPC surface: the shared codec, the handler
//     options every procedure is registered with, and the procedure table.
//   - handler.go and handler_rpc.go hold the handlers those routers mount, and
//     nothing else — no routing, no mount order.
//   - server.go binds the listener to the router.
//   - middleware/ holds the pipeline pieces, static/ the uploads mount.
//
// This file holds what the package as a whole needs.
package transport

import (
	"fmt"
	"net/http"

	"github.com/riipandi/tango/internal/config"
)

// rateLimitExclusions are the API path prefixes the rate limiter never
// counts. One entry per line, the reason beside it. A prefix matches the
// paths under it, so "/api/healthz" also spares "/api/healthz/deep"; the
// limiter itself runs on the API surface only, so a static asset or a
// metrics scrape never reaches a check in the first place.
var rateLimitExclusions = []string{
	"/api/healthz", // liveness probes and load-balancer checks
}

// NewServer builds the HTTP server the router is served through, with the
// timeouts the configuration holds. The caller owns the listener and the
// graceful drain; the server here is only the bound handler and its timeouts.
//
// It reads the same `server` section the router's request deadline is read
// from, so a timeout is decided in one place on both sides of the boundary.
func NewServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           handler,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
