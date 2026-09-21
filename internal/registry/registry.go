// Package registry is the composition root: the one place the shared
// dependencies are registered with samber/do and wired onto each other. It is
// the only package that names constructors together, so a wiring change happens
// in one reviewable file.
//
// Services are lazy: the pool and the health checker are built when the first
// service that needs them is invoked, which is the moment serve resolves the
// server. A dependency that cannot be built — a database that is down, a data
// directory that cannot be used — fails that invocation, before the listener
// opens.
package registry

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	do "github.com/samber/do/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/transport"
)

// New registers the shared services of a serve run. The metrics handler comes
// from the caller because the observer is built and closed by the command
// lifecycle, not by the container.
func New(ctx context.Context, cfg config.Config, metrics http.Handler) *do.RootScope {
	injector := do.New()

	do.ProvideValue(injector, &cfg)

	do.Provide(injector, func(i do.Injector) (*datastore.Postgres, error) {
		c := do.MustInvoke[*config.Config](i)
		return datastore.NewPostgres(ctx, datastore.PostgresOptions{
			DSN:             c.Database.URL,
			ApplicationName: config.AppIdentifier,
			SearchPath:      c.Database.SearchPath,
			Timezone:        c.Database.Timezone,
			MaxConns:        c.Database.MaxConns,
			MinConns:        c.Database.MinConns,
			MaxConnLifetime: c.Database.MaxConnLifetime,
			MaxConnIdleTime: c.Database.MaxConnIdleTime,
			ConnectTimeout:  c.Database.ConnectTimeout,
		})
	})

	do.Provide(injector, func(i do.Injector) (*health.Checker, error) {
		c := do.MustInvoke[*config.Config](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		return health.NewChecker(
			health.WithChecks(
				health.PostgresCheck(pool, config.RedactDSN(c.Database.URL)),
				health.StorageCheck(c.Storage.LocalPath),
			),
			health.WithInfo(map[string]string{
				"version": config.AppVersion,
				"mode":    c.App.Mode,
			}),
			health.WithInfoFunc(uptime),
		), nil
	})

	do.Provide(injector, func(i do.Injector) (chi.Router, error) {
		c := do.MustInvoke[*config.Config](i)
		checker := do.MustInvoke[*health.Checker](i)
		return transport.NewRouter(transport.Options{
			Config:  *c,
			Checker: checker,
			Metrics: metrics,
		}), nil
	})

	do.Provide(injector, func(i do.Injector) (*http.Server, error) {
		c := do.MustInvoke[*config.Config](i)
		router := do.MustInvoke[chi.Router](i)
		return transport.NewServer(*c, router), nil
	})

	return injector
}

// uptime is the computed health metadata: how long the process has been up.
func uptime(_ context.Context) map[string]string {
	return map[string]string{"uptime": time.Since(startTime).Round(time.Second).String()}
}

// startTime is when this process began, read once so every health report
// measures the same clock.
var startTime = time.Now()
