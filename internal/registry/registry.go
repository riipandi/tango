// Package registry is the composition root: the one place the shared
// dependencies are registered with samber/do and wired onto each other.
//
// The package is split along one boundary:
//
//   - infrastructure.go registers what the process runs on — the pool, the
//     cache, the queue, the storage engine, the outbound client. It names no
//     module, and importing one there would be visible in a one-line diff.
//   - modules.go registers what the application mounts. An area owns its own
//     wiring and reaches infrastructure only by invoking it from the
//     container, so this file never names an area's internals — it holds the
//     list, and a consumer can append to it.
//   - this file joins the two. The router and the server are the only things
//     that need both, so they are the only ones that name both.
//
// Each half is a `do.Package`, so `do.New` reads as the composition list
// itself: the values the command owns, the infrastructure, the modules.
//
// Services are lazy: the pool and the health checker are built when the first
// service that needs them is invoked, which is the moment serve resolves the
// server. A dependency that cannot be built — a database that is down, a data
// directory that cannot be used — fails that invocation, before the listener
// opens.
package registry

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/samber/do/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/internal/transport/middleware"
)

// New registers the shared services of a serve run.
//
// The configuration and the logger are registered as values because the command
// lifecycle owns them; they are services like any other, so a provider below
// resolves them instead of closing over them. The metrics handler is the one
// exception: it is built by the observer the command holds, so it is captured
// here rather than registered, and only the router reads it.
//
// The areas of this application are mounted, followed by any the caller
// passes. A consumer outside this repository therefore serves its own area
// without editing this package — it appends to the list, and the last one to
// claim a route wins.
func New(ctx context.Context, cfg config.Config, metrics http.Handler, logger *slog.Logger, extra ...Area) *do.RootScope {
	areas := append(Areas(), extra...)

	return do.New(
		do.Eager(&cfg),
		do.Eager(logger),
		infrastructure(ctx),
		areaPackages(areas),
		do.Lazy(func(i do.Injector) (chi.Router, error) {
			return newRouter(i, metrics, areas)
		}),
		do.Lazy(newServer),
	)
}

// newRouter builds the HTTP surface: the API, the static mount, and the
// modules mounted on the root router.
//
// This is the join point, so it is the one provider that resolves from both
// halves — the health checker and the limiter come from infrastructure, the
// module list from the areas.
func newRouter(i do.Injector, metrics http.Handler, areas []Area) (chi.Router, error) {
	c := do.MustInvoke[*config.Config](i)
	checker := do.MustInvoke[*health.Checker](i)
	log := do.MustInvoke[*slog.Logger](i)
	limiter := do.MustInvoke[middleware.Limiter](i)

	mounted, err := mountAreas(i, areas)
	if err != nil {
		return nil, err
	}

	return transport.NewRouter(transport.Options{
		Config:      *c,
		Checker:     checker,
		Metrics:     metrics,
		Logger:      log,
		RateLimiter: limiter,
		Modules:     mounted,
	}), nil
}

// newServer wraps the router in the configured HTTP server.
func newServer(i do.Injector) (*http.Server, error) {
	c := do.MustInvoke[*config.Config](i)
	router := do.MustInvoke[chi.Router](i)
	return transport.NewServer(*c, router), nil
}

// uptime is the computed health metadata: how long the process has been up.
func uptime(_ context.Context) map[string]string {
	return map[string]string{"uptime": time.Since(startTime).Round(time.Second).String()}
}

// startTime is when this process began, read once so every health report
// measures the same clock.
var startTime = time.Now()
