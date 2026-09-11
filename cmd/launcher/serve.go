package launcher

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/transport"
	"github.com/spf13/cobra"
)

var serveHost string
var servePort string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the application server",
	Run: func(cmd *cobra.Command, args []string) {
		config.ApplyFlags(serveHost, servePort)

		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
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
			log.Fatalf("failed to build logger: %v", err)
		}
		defer logCloser.Close()

		reg := registry.New(registry.Deps{Config: cfg, Logger: lg})
		if err := reg.Start(cmd.Context()); err != nil {
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
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
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
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Host to bind to")
	serveCmd.Flags().StringVar(&servePort, "port", "", "Port to bind to")
}
