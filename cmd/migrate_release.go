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
	Description: `Applies the migrations embedded in the binary, in version order.
Migrations already recorded in the app_migration table are skipped.
A concurrent run is safe: the migrator takes a Postgres session 
advisory lock the duration.`,
	Flags: []cli.Flag{
		&cli.Uint64Flag{
			Name:        "to",
			Usage:       "Apply migrations only up to this version",
			HideDefault: true,
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "List the pending migrations without applying them",
		},
		&cli.BoolFlag{
			Name:  "force",
			Usage: "Skip the confirmation prompt",
		},
	},
	Action: runMigrateUp,
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
			Usage: "Print the migration that would be rolled back without changing anything",
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
