package launcher

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/web"
)

type ServeCmd struct {
	Host string `help:"Host to bind to"`
	Port string `help:"Port to bind to"`
}

// flagOverrides maps serve flags to config keys.
// Bad --port is a plain error, never log.Fatal (skips defers).
func flagOverrides(host, port string) (map[string]any, error) {
	overrides := map[string]any{}
	if host != "" {
		overrides["host"] = host
	}
	if port != "" {
		cleaned := strings.TrimLeft(port, ":")
		parsed, err := strconv.Atoi(cleaned)
		if err != nil {
			return nil, fmt.Errorf("invalid --port value %q: %w", port, err)
		}
		overrides["port"] = parsed
	}
	return overrides, nil
}

// rateLimiter adapts the datastore pool to the transport limiter
// contract; a nil return degrades to an unthrottled server (tests).
func rateLimiter(db *datastore.Postgres) func(http.Handler) http.Handler {
	return middleware.RateLimit(db)
}

// latestVersion exposes the cached release feed from the jobs module
// for /api/version/latest; a nil source keeps the deployed version.
func latestVersion(feed *jobs.Registry) transport.LatestVersionSource {
	if feed == nil {
		return nil
	}
	return feed
}

// Run starts the server and shuts it down on signal.
func (s *ServeCmd) Run(cli *CLI) error {
	overrides, err := flagOverrides(s.Host, s.Port)
	if err != nil {
		return err
	}
	cfg, err := loadConfig(cli, overrides)
	if err != nil {
		return err
	}

	// Logger is shared by all modules; close drains its queue.
	lg, logCloser, err := logger.New(logger.Options{
		Level:  cfg.App.LogLevel,
		Output: cfg.App.LogTransport,
		Format: cfg.App.LogFormat,
		File:   cfg.LogFile(),
	})
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer logCloser.Close()

	// Fetcher is shared and closed after the server.
	fch := fetcher.New(fetcher.Options{Logger: lg})
	defer fch.Close()

	// Database is pinged at startup and closed before fetcher and logger.
	db, err := datastore.New(context.Background(), datastore.Options{DSN: cfg.Database.URL})
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer db.Close()

	// Schema version check: warn (don't fail) when the database is behind the compiled-in
	// migration target; operators run `tango db migrate:up` explicitly.
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 15*time.Second)
	current, target, err := database.MigrateVersion(checkCtx, cfg.Database.URL)
	checkCancel()
	switch {
	case err != nil:
		lg.WithError(err).Warn("schema version check failed")
	case current < target:
		lg.Warn(fmt.Sprintf(
			"database schema is behind: applied version %d, target %d — run `%s db migrate:up`",
			current, target, config.AppName))
	}

	// Mount embedded React Email templates.
	templates, err := fs.Sub(web.EmailTemplates, "email")
	if err != nil {
		return fmt.Errorf("mount email templates: %w", err)
	}
	ml := mailer.New(cfg.Mailer, mailer.Options{
		Templates: templates,
		Logger:    lg,
	})

	rt, err := registry.New(registry.Deps{Config: cfg, Logger: lg, Fetcher: fch, Mailer: ml, DB: db})
	if err != nil {
		return fmt.Errorf("build runtime: %w", err)
	}

	// SMTP relay settings become admin-editable: the mailer resolves
	// them per send from the appconfig surface (env values stay the
	// default layer). Late-bound — the module exists after New.
	if setter, ok := ml.(mailer.SettingsSourceSetter); ok {
		fallback := cfg.Mailer
		setter.SetSettingsSource(func(ctx context.Context) (config.MailerConfig, error) {
			values, err := rt.AppConfig.MergedValues(ctx)
			if err != nil {
				return fallback, err
			}
			return mailer.SettingsFromValues(values, fallback), nil
		})
	}
	if err := rt.Start(context.Background()); err != nil {
		return fmt.Errorf("start modules: %w", err)
	}

	srv := transport.NewHTTPServer(transport.RouteSet{MountRoot: rt.MountRoot, MountAPI: rt.MountAPI, RequireSession: rt.SessionGuard()}, cfg, lg, rateLimiter(db), latestVersion(rt.Jobs))
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	serveErr := make(chan error, 1)
	go func() {
		lg.Info("listening on http://" + addr)
		if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			serveErr <- err
		} else {
			serveErr <- nil
		}
	}()

	// Preserve the received signal as the context cause.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serveErr:
		// Startup failure (port busy, listener error) must surface
		// immediately, not wait for a signal that never comes.
		return err
	case <-ctx.Done():
		lg.WithError(context.Cause(ctx)).Info("received shutdown signal")
	}

	// A second signal means "get out now": exit hard, skipping the
	// drain and deferred closes.
	force := make(chan os.Signal, 1)
	signal.Notify(force, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(force)
	go func() {
		<-force
		lg.Warn("second shutdown signal — exiting immediately")
		os.Exit(1)
	}()

	lg.Info("shutting down server...")

	// HTTP drain and module stop get independent budgets: a slow
	// client must not eat the time the queue needs to drain tasks.
	httpCtx, httpCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer httpCancel()
	if err := srv.Shutdown(httpCtx); err != nil {
		lg.WithError(err).Warn("http drain exceeded budget; stopping modules anyway")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopCancel()
	if err := rt.Stop(stopCtx); err != nil {
		lg.WithError(err).Warn("module shutdown errors")
	}

	lg.Info("server stopped")
	return <-serveErr
}
