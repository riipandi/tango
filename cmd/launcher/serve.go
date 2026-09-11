package launcher

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/transport"
)

// ServeCmd starts the application server.
type ServeCmd struct {
	Host string `help:"Host to bind to"`
	Port string `help:"Port to bind to"`
}

// Run loads the layered config, builds the shared logger, and serves
// until SIGINT/SIGTERM.
func (s *ServeCmd) Run(cli *CLI) error {
	cfg, err := config.Load(config.LoadOptions{
		EnvFile:   cli.EnvFile,
		Overrides: flagOverrides(s.Host, s.Port),
	})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Composition root of the runtime: the application logger is
	// built once from config and injected everywhere (registry,
	// transport). Closing it drains the async queue on shutdown.
	lg, logCloser, err := logger.New(logger.Options{
		Level:  cfg.App.LogLevel,
		Output: cfg.App.LogTransport,
		Format: cfg.App.LogFormat,
		File:   cfg.App.LogFile,
	})
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	defer logCloser.Close()

	// Shared outbound client for service integrations; closed
	// last so shutdown-path calls still have a live pool.
	fch := fetcher.New(fetcher.Options{Logger: lg})
	defer fch.Close()

	reg := registry.New(registry.Deps{Config: cfg, Logger: lg, Fetcher: fch})
	if err := reg.Start(context.Background()); err != nil {
		lg.WithError(err).Fatal("failed to start modules")
	}

	srv := transport.NewHTTPServer(reg, cfg, lg)
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	go func() {
		lg.Info("listening on http://" + addr)
		if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
			lg.WithError(err).Fatal("server error")
		}
	}()

	// signal.NotifyContext (Go 1.26+): the returned context is
	// canceled with the received signal as its cause, so the
	// shutdown path can report exactly which signal arrived.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	lg.WithError(context.Cause(ctx)).Info("received shutdown signal")

	lg.Info("shutting down server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		lg.WithError(err).Fatal("shutdown error")
	}

	if err := reg.Stop(shutdownCtx); err != nil {
		lg.WithError(err).Warn("module shutdown errors")
	}

	lg.Info("server stopped")
	return nil
}

// flagOverrides resolves the serve CLI flags into config overrides.
// Empty flags contribute nothing; an invalid --port fails fast.
func flagOverrides(host, port string) map[string]any {
	overrides := map[string]any{}
	if host != "" {
		overrides["host"] = host
	}
	if port != "" {
		cleaned := strings.TrimLeft(port, ":")
		parsed, err := strconv.Atoi(cleaned)
		if err != nil {
			log.Fatalf("invalid --port value: %q", port)
		}
		overrides["port"] = parsed
	}
	return overrides
}
