//go:build debug

package main

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/urfave/cli/v3"
)

var rootCmd = &cli.Command{
	Name:            config.AppName,
	Description:     config.Description,
	Version:         config.AppVersion,
	HideVersion:     true,
	HideHelpCommand: true,
	Commands: []*cli.Command{
		serveCmd,
		migrateUpCmd,
		migrateDownCmd,
		migrateStatusCmd,
		migrateVersionCmd,
		migrateCreateCmd,
		migrateValidateCmd,
		migrateResetCmd,
		migrateSeedCmd,
		dbExportCmd,
		dbImportCmd,
		keyGenerateCmd,
		keyRotateCmd,
		healthCheckCmd,
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "env-file",
			Usage:    "Load environment variables from a file",
			Required: false,
		},
		&cli.StringFlag{
			Name:     "data-dir",
			Usage:    "Set the Application data directory",
			Required: false,
			Value:    config.DefaultDataDir,
		},
		&cli.BoolFlag{
			Name:    "version",
			Usage:   "Show the application version",
			Aliases: []string{"V"},
		},
	},
}
