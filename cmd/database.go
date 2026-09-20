package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var dbExportCmd = &cli.Command{
	Name:     "db:export",
	Category: "Database operation",
	Usage:    "Export the application database",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:        "format",
			Usage:       "Export format",
			DefaultText: "sql",
			Value:       "sql",
		},
		&cli.BoolFlag{
			Name:  "schema-only",
			Usage: "Export only the schema (no data)",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

var dbImportCmd = &cli.Command{
	Name:     "db:import",
	Category: "Database operation",
	Usage:    "Import database from backup file",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
