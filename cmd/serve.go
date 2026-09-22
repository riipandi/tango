package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/samber/do/v2"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/scheduler"
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

		// The container wires the pool, the health checker, the queue, and the
		// server, so the command only names what it blocks on. Resolving the
		// server is what opens the pool: an unreachable database fails the run
		// here, before the listener opens.
		injector := registry.New(ctx, cfg, obs.MetricsHandler(), log.Slog())
		server, err := do.Invoke[*http.Server](injector)
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}

		// The queue starts with the server and stops with the injector: its
		// Shutdown drains the in-flight tasks after the HTTP drain ends.
		queueClient, err := do.Invoke[*queue.Client](injector)
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		queueClient.Start(ctx)

		// The scheduler fires after the queue it enqueues onto, so its first
		// tick claims into a running dispatcher; it stops before the drain,
		// and its fires in flight join the queue's own drain.
		jobScheduler, err := do.Invoke[*scheduler.Scheduler](injector)
		if err != nil {
			return fmt.Errorf("serve: %w", err)
		}
		jobScheduler.Start(ctx)

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
			"workers", cfg.Queue.NumWorkers,
			"address", server.Addr)

		select {
		case err := <-serveErr:
			return err
		case <-ctx.Done():
		}

		// The drain window bounds everything the run waits for at shutdown:
		// the scheduler's fires, the listener's requests, the queue's tasks.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cfg.Server.ShutdownTimeout)
		defer cancel()

		// The scheduler stops before the HTTP drain: a fire in flight
		// finishes its enqueue — an enqueued task is durable, so the queue's
		// own drain after this cannot lose one — and no new fire starts
		// while the listener is closing.
		if !jobScheduler.Stop(shutdownCtx) {
			log.Slog().WarnContext(ctx, "serve: scheduler left fires running")
		}

		// Shutdown closes the listener first, so a request that arrives during
		// the drain is refused rather than served by a server the caller gave
		// up on, then waits for the in-flight work, bounded by the configured
		// drain window.
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("serve: drain: %w", err)
		}

		// The pool outlives the listener, so it is closed after the drain: a
		// request still finishing needs its connection until the drain ends.
		if report := injector.ShutdownWithContext(shutdownCtx); report != nil && !report.Succeed {
			slog.ErrorContext(ctx, "serve: release", "err", report.Error())
		}
		return nil
	},
}
