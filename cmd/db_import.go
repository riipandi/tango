package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var dbImportCmd = &cli.Command{
	Name:     "db:import",
	Category: "Database operation",
	Usage:    "Import database from backup file",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
