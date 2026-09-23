// Package transport serves the application over HTTP: the router that mounts
// the API, the health surface, and the SPA; the server that binds it.
package transport

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/web"
)

// Options is what the router needs to serve. Every field is explicit, so a
// caller cannot construct a router whose dependencies it cannot name.
type Options struct {
	// Config is the resolved configuration. The router reads the CORS policy,
	// the Prometheus path, and the application identity from it.
	Config config.Config
	// Checker reports the health the /api/healthz endpoint publishes. A nil
	// checker leaves the endpoint out, rather than publishing a result
	// nothing backed.
	Checker *health.Checker
	// Metrics is the Prometheus exposition handler. A nil handler mounts no
	// metrics endpoint, which is the state a disabled signal is in.
	Metrics http.Handler
	// Logger is the process logger the request middleware writes through. A
	// nil logger leaves the request and panic middleware out, which is the
	// state a test that reads only responses is in.
	Logger *slog.Logger
	// RateLimiter is the limiter the API surface is throttled by. A nil
	// limiter mounts no throttling, which is the state a run without a
	// rate_limit driver is in.
	RateLimiter middleware.Limiter
	// Modules are the feature modules whose routes the server mounts.
	Modules []kernel.Module
}

// NewRouter builds the request pipeline: request id first, so every response
// and log line can name its request; the request logger and the panic
// recovery around every route; CORS, so a policy question is answered before
// a route runs; the request timeout; then the endpoints.
//
// The rate limiter wraps the routes a client calls, and a static asset or a
// metrics scrape is outside it: the budget belongs to the API, not to the
// page that embeds it. The throttled surface is a chi group rather than the
// /api subrouter, because a module mounts its own routes — a feature under
// /api, a protocol endpoint at /.well-known — on the router it is handed, and
// middleware attached to the /api subrouter never reaches them. The group is
// what makes the limiter cover every mounted route without also covering the
// SPA. Paths that must never be throttled are listed in rateLimitExclusions
// above.
//
// The SPA is mounted last: its handler answers whatever the routes above it
// did not claim, and its own not-found rule keeps API and protocol paths from
// being answered with index.html.
func NewRouter(opts Options) chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Logger(opts.Logger))
	r.Use(middleware.Recoverer(opts.Logger))
	r.Use(middleware.CORS(opts.Config.Server.CORS))
	r.Use(middleware.Timeout(opts.Config.Server.WriteTimeout))

	if opts.Metrics != nil {
		r.Handle(opts.Config.OTEL.Metrics.PrometheusPath, opts.Metrics)
	}

	r.Group(func(throttled chi.Router) {
		if opts.RateLimiter != nil {
			throttled.Use(middleware.RateLimit(opts.RateLimiter, rateLimitExclusions...))
		}

		throttled.Route("/api", func(api chi.Router) {
			api.Get("/", apiRoot(opts.Config))
			if opts.Checker != nil {
				api.Get("/healthz", health.Handler(opts.Checker))
			}
		})

		// The modules mount inside the group, so every route a module claims
		// is throttled by the same policy as the API's own.
		kernel.Mount(throttled, opts.Modules...)
	})

	web.SetupStatic(r)
	return r
}

// apiRoot is the /api landing endpoint. It names what is served, so a probe
// that reached the API can report the surface it found without reading docs.
func apiRoot(cfg config.Config) http.HandlerFunc {
	type apiRootData struct {
		Name    string `json:"name"`
		Version string `json:"version"`
		Mode    string `json:"mode"`
	}

	return func(w http.ResponseWriter, r *http.Request) {
		responder.Success(w, r, http.StatusOK, apiRootData{
			Name:    config.AppIdentifier,
			Version: config.AppVersion,
			Mode:    cfg.App.Mode,
		})
	}
}
