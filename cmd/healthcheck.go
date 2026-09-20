package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var healthCheckCmd = &cli.Command{
	Name:    "health",
	Usage:   "Check application health status",
	Aliases: []string{"hc"},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
