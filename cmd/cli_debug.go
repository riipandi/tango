//go:build debug

package main

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/urfave/cli/v3"
)

var rootCmd = &cli.Command{
	Name:            config.AppIdentifier,
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
		configGenerateCmd,
		configValidateCmd,
		configPrintCmd,
	},
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     config.FlagEnvFile,
			Usage:    "Load environment variables from a file",
			Required: false,
		},
		&cli.StringFlag{
			Name:     config.FlagConfigFile,
			Usage:    "Load configuration from a JSON file",
			Required: false,
		},
		&cli.BoolFlag{
			Name:    "version",
			Usage:   "Show the application version",
			Aliases: []string{"V"},
		},
	},
	Before: initConfig,
}
