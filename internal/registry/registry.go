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
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/samber/do/v2"

	"github.com/riipandi/tango/internal/cache"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/scheduler"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/crypto"
)

// New registers the shared services of a serve run. The metrics handler and
// the logger come from the caller because both are built and closed by the
// command lifecycle, not by the container; they are registered as values so
// the services below resolve them like any other dependency.
func New(ctx context.Context, cfg config.Config, metrics http.Handler, logger *slog.Logger) *do.RootScope {
	injector := do.New()

	do.ProvideValue(injector, &cfg)
	do.ProvideValue(injector, logger)

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
		log := do.MustInvoke[*slog.Logger](i)
		limiter := do.MustInvoke[middleware.Limiter](i)
		return transport.NewRouter(transport.Options{
			Config:      *c,
			Checker:     checker,
			Metrics:     metrics,
			Logger:      log,
			RateLimiter: limiter,
		}), nil
	})

	do.Provide(injector, func(i do.Injector) (*datastore.Valkey, error) {
		c := do.MustInvoke[*config.Config](i)
		return datastore.NewValkey(ctx, datastore.ValkeyOptions{
			URL:             c.KVStore.URL,
			DB:              c.KVStore.DB,
			ApplicationName: config.AppIdentifier,
		})
	})

	do.Provide(injector, func(i do.Injector) (cache.Cache, error) {
		c := do.MustInvoke[*config.Config](i)
		// The backend client is resolved only while it is enabled: a run
		// without it never opens a connection, and the cache factory
		// answers the missing client with the no-op driver.
		var kv *datastore.Valkey
		if c.KVStore.Enable {
			kv = do.MustInvoke[*datastore.Valkey](i)
		}
		return cache.New(*c, kv), nil
	})

	do.Provide(injector, func(i do.Injector) (*queue.Client, error) {
		c := do.MustInvoke[*config.Config](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		log := do.MustInvoke[*slog.Logger](i)
		var encryptor *crypto.Cipher
		if c.Queue.Encrypt {
			// Validation refuses an encrypted queue without a usable secret,
			// so a failing parse here is a broken deployment, not a silent
			// switch to plaintext.
			cipher, err := crypto.NewCipherFromHex(c.App.SecretKey)
			if err != nil {
				return nil, err
			}
			encryptor = cipher
		}
		client, err := queue.NewClient(queue.ClientConfig{
			Store:        pool,
			Logger:       log,
			NumWorkers:   c.Queue.NumWorkers,
			ReleaseAfter: c.Queue.ReleaseAfter,
			Encryptor:    encryptor,
		})
		if err != nil {
			return nil, err
		}

		// The job list is registered beside the engine it runs on: a queue
		// with no jobs is a worker pool with nothing to do.
		if err := jobs.Register(ctx, client, c.Queue.CleanupInterval); err != nil {
			return nil, err
		}
		return client, nil
	})

	do.Provide(injector, func(i do.Injector) (*scheduler.Scheduler, error) {
		c := do.MustInvoke[*config.Config](i)
		pool := do.MustInvoke[*datastore.Postgres](i)
		client := do.MustInvoke[*queue.Client](i)
		log := do.MustInvoke[*slog.Logger](i)
		location, err := time.LoadLocation(c.Scheduler.Timezone)
		if err != nil {
			return nil, err
		}
		return scheduler.New(scheduler.Config{
			Store:    pool,
			Client:   client,
			Logger:   log,
			Location: location,
			Jobs:     jobs.Scheduled(),
		})
	})

	do.Provide(injector, func(i do.Injector) (middleware.Limiter, error) {
		c := do.MustInvoke[*config.Config](i)
		switch c.RateLimit.Driver {
		case config.RateLimitDB:
			pool := do.MustInvoke[*datastore.Postgres](i)
			return middleware.NewDatabaseLimiter(pool, c.RateLimit), nil
		case config.RateLimitKV:
			// Validation refuses a kvstore driver while the backend is
			// disabled, so resolving the client here is always a run that
			// asked for it.
			kv := do.MustInvoke[*datastore.Valkey](i)
			return middleware.NewKVStoreLimiter(kv.Client(), c.RateLimit), nil
		default:
			return nil, fmt.Errorf("registry: rate_limit.driver: unknown driver %q", c.RateLimit.Driver)
		}
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
