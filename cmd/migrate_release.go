package main

import (
	"context"
	"fmt"

	"github.com/urfave/cli/v3"
)

var migrateUpCmd = &cli.Command{
	Name:     "migrate:up",
	Category: "Database operation",
	Usage:    "Run database migrations",
	Flags: []cli.Flag{
		&cli.UintFlag{
			Name:        "to",
			Usage:       "Apply migrations only up to this version",
			HideDefault: true,
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print what would be seeded without changing anything",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

var migrateDownCmd = &cli.Command{
	Name:     "migrate:down",
	Category: "Database operation",
	Usage:    "Rollback database migrations",
	Flags: []cli.Flag{
		&cli.UintFlag{
			Name:        "count",
			Usage:       "Number of migrations to roll back",
			DefaultText: "1",
			Value:       1,
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Print what would be seeded without changing anything",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

var migrateStatusCmd = &cli.Command{
	Name:     "migrate:status",
	Category: "Database operation",
	Usage:    "Check database migration status",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}

var migrateVersionCmd = &cli.Command{
	Name:     "migrate:version",
	Category: "Database operation",
	Usage:    "Print the current migration version",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		fmt.Println("not yet implemented")
		return nil
	},
}
