package launcher

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/transport"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/appconfig"
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
	return middleware.RateLimit(db, middleware.RateClassDefault)
}

// latestVersion exposes the cached release feed from the jobs module
// for /api/version/latest; a nil source keeps the deployed version.
func latestVersion(reg *kernel.Registry) transport.LatestVersionSource {
	source, ok := reg.Get("jobs").(transport.LatestVersionSource)
	if !ok {
		return nil
	}
	return source
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

	reg := registry.New(registry.Deps{Config: cfg, Logger: lg, Fetcher: fch, Mailer: ml, DB: db})

	// SMTP relay settings become admin-editable: the mailer resolves
	// them per send from the appconfig surface (env values stay the
	// default layer). Late-bound — the module exists after New.
	if module, ok := reg.Get(appconfig.ModuleName).(*appconfig.Module); ok {
		if setter, ok := ml.(mailer.SettingsSourceSetter); ok {
			fallback := cfg.Mailer
			setter.SetSettingsSource(func(ctx context.Context) (config.MailerConfig, error) {
				values, err := module.MergedValues(ctx)
				if err != nil {
					return fallback, err
				}
				return mailer.SettingsFromValues(values, fallback), nil
			})
		}
	}
	if err := reg.Start(context.Background()); err != nil {
		return fmt.Errorf("start modules: %w", err)
	}

	srv := transport.NewHTTPServer(reg, cfg, lg, rateLimiter(db), latestVersion(reg))
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
	<-ctx.Done()
	lg.WithError(context.Cause(ctx)).Info("received shutdown signal")

	lg.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown server: %w", err)
	}

	if err := reg.Stop(shutdownCtx); err != nil {
		lg.WithError(err).Warn("module shutdown errors")
	}

	lg.Info("server stopped")
	return <-serveErr
}
