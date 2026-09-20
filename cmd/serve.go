package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var serveCmd = &cli.Command{
	Name:  "serve",
	Usage: "Start the application server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "host",
			Usage:       "Host to bind to",
			DefaultText: "0.0.0.0",
			Value:       "0.0.0.0",
		},
		&cli.UintFlag{
			Name:        "port",
			Usage:       "Port to bind to",
			DefaultText: "3080",
			Value:       3080,
		},
		&cli.StringFlag{
			Name:  "base-url",
			Usage: "Base URL (e.g. http://localhost:3080)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
