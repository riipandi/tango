package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/urfave/cli/v3"
)

var serveCmd = &cli.Command{
	Name:  "serve",
	Usage: "Start the application server",
	Description: `Starts the application HTTP server.
--host, --port, and --base-url override the config file for this run,
so a value can be tried without editing the file. Without them the
file decides.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "host",
			Usage:       "Host to bind to",
			DefaultText: "0.0.0.0",
		},
		&cli.UintFlag{
			Name:        "port",
			Usage:       "Port to bind to",
			DefaultText: "3080",
		},
		&cli.StringFlag{
			Name:  "base-url",
			Usage: "Base URL (e.g. http://localhost:3080)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// serve is the one command that uses most of the configuration, so it is
		// where the whole configuration is checked: a server that starts on a
		// half-configured setup fails later, in a place far from the mistake.
		cfg, err := fullConfigFrom(ctx)
		if err != nil {
			return err
		}

		// The process logger is built here, before anything else runs, so every
		// line the server emits from now on goes through the configured
		// transports. It is closed by the root After, which flushes whatever is
		// still queued.
		log, err := loggerFrom(ctx)
		if err != nil {
			return err
		}

		// The observer is built beside the logger, for the same reason: a signal
		// that is switched on has to be reporting before the first request
		// arrives, and its queues are drained by the root After.
		obs, err := observerFrom(ctx)
		if err != nil {
			return err
		}

		// The metrics exposition is the one surface serve can host before
		// internal/transport exists: the Prometheus bridge already holds the
		// registry, and a disabled signal exposes no handler at all rather than
		// an endpoint that reports nothing.
		mux := http.NewServeMux()
		if handler := obs.MetricsHandler(); handler != nil {
			mux.Handle(cfg.OTEL.Metrics.PrometheusPath, handler)
		}

		server := &http.Server{
			Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
			Handler:           mux,
			ReadTimeout:       cfg.Server.ReadTimeout,
			ReadHeaderTimeout: cfg.Server.ReadTimeout,
			WriteTimeout:      cfg.Server.WriteTimeout,
			IdleTimeout:       cfg.Server.IdleTimeout,
		}

		serveErr := make(chan error, 1)
		go func() {
			if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				serveErr <- err
			}
		}()

		log.Slog().InfoContext(ctx, "starting",
			"mode", cfg.App.Mode,
			"transport", cfg.Log.Transport,
			"protocol", cfg.OTEL.Protocol,
			"tracing", obs.Tracing(),
			"metrics", obs.Metrics(),
			"address", server.Addr)

		select {
		case err := <-serveErr:
			return err
		case <-ctx.Done():
		}

		// Shutdown closes the listener first, so a request that arrives during
		// the drain is refused rather than served by a server the caller gave
		// up on, then waits for the in-flight work, bounded by the configured
		// drain window.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("serve: drain: %w", err)
		}
		return nil
	},
}
