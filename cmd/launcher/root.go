package launcher

import (
	"fmt"
	"os"

	"github.com/riipandi/tango/internal/config"
	"github.com/spf13/cobra"
)

var argVersionShort bool
var argVersionSemantic bool

var rootCmd = &cobra.Command{
	Use:   "tango",
	Short: "Fullstack Go + React application",
	Long:  "A fullstack web application built with Go, Chi, and React.",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show the application version",
	Run: func(cmd *cobra.Command, args []string) {
		if argVersionShort {
			cmd.Printf("%s (%s)\n", config.AppVersion, config.BuildHash)
			return
		} else if argVersionSemantic {
			cmd.Printf("%s\n", config.AppVersion)
			return
		} else {
			cmd.Printf("%s %s %s (%s %s)\n", config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate)
		}
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	config.Init()

	// Set `true` to disable the default help subcommand
	rootCmd.SetHelpCommand(&cobra.Command{Hidden: false})

	// Add version subcommand
	rootCmd.AddCommand(versionCmd)
	versionCmd.Flags().BoolVarP(&argVersionShort, "short", "s", false, "Show short version")
	versionCmd.Flags().BoolVarP(&argVersionSemantic, "semantic", "S", false, "Show semantic version")

	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(migrateCmd)
	rootCmd.AddCommand(healthCmd)
}
