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

// rateLimitExclusions are the API path prefixes the rate limiter never
// counts. One entry per line, the reason beside it. A prefix matches the
// paths under it, so "/api/healthz" also spares "/api/healthz/deep"; the
// limiter itself runs on the API surface only, so a static asset or a
// metrics scrape never reaches a check in the first place.
var rateLimitExclusions = []string{
	"/api/healthz", // liveness probes and load-balancer checks
}
