package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var keyGenerateCmd = &cli.Command{
	Name:  "key:generate",
	Usage: "Generate new application encryption keys",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

var keyRotateCmd = &cli.Command{
	Name:  "key:rotate",
	Usage: "Rotate encryption keys and re-encrypts data",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
