package main

import (
	"context"
	"fmt"

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
		log.Slog().Info("starting",
			"mode", cfg.App.Mode,
			"transport", cfg.Log.Transport,
			"address", fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port))

		fmt.Println("not yet implemented")
		return nil
	},
}
