package transport

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/internal/transport/static"
	"github.com/riipandi/tango/web"
)

// Options is what the routers need to serve. Every field is explicit, so a
// caller cannot construct a router whose dependencies it cannot name.
type Options struct {
	// Config is the resolved configuration. The routers read the CORS policy,
	// the Prometheus path, and the application identity from it.
	Config config.Config
	// Checker reports the health the `/api/healthz` endpoint and the health
	// procedure publish. A nil checker leaves both out, rather than publishing
	// a result nothing backed.
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
	// Modules are the feature modules whose routes and procedures the server
	// mounts.
	Modules []kernel.Module
}

// NewRouter builds the HTTP surface: the request pipeline, then the endpoints
// it mounts, then the ConnectRPC surface, the uploads, and the SPA last.
//
// The pipeline runs request id first, so every response and log line can name
// its request; then the request logger and the panic recovery around every
// route; then CORS, so a policy question is answered before a route runs; then
// the request timeout.
//
// The rate limiter wraps the routes a client calls, and a static asset or a
// metrics scrape is outside it: the budget belongs to the API, not to the page
// that embeds it. The throttled surface is a chi group rather than the /api
// subrouter, because a module mounts its own routes — a feature under /api, a
// protocol endpoint at /.well-known — on the router it is handed, and
// middleware attached to the /api subrouter never reaches them. The group is
// what makes the limiter cover every mounted route without also covering the
// SPA. Paths that must never be throttled are listed in rateLimitExclusions.
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

		// The ConnectRPC surface is composed in rpc.go and mounted inside the
		// same group, so a procedure call is held to the same rate policy as
		// a REST route. It mounts before the modules: a module that claims a
		// path under the RPC prefix would be a defect, and chi reports the
		// conflict at startup rather than answering two handlers for one path.
		mountRPC(throttled, opts.Checker, opts.Modules)

		// The modules mount inside the group too, so every route a module
		// claims is throttled by the same policy as the API's own.
		kernel.Mount(throttled, opts.Modules...)
	})

	// The uploads are served outside the group: a page that loads an image
	// spends no rate-limit check, the budget belonging to the API a client
	// calls rather than to the assets it renders. It is mounted before the
	// SPA, whose not-found handler would otherwise answer a missing upload
	// with index.html.
	static.Mount(r, static.NewLocal(static.Dir(opts.Config.Storage.LocalPath)))

	web.SetupStatic(r)
	return r
}
