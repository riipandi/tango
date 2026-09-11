// Package launcher implements the tango command line.
//
// The command surface is declared once as a struct (kong); runtime
// configuration is layered separately by internal/config (koanf).
// Kong flags stay zero-valued — config defaults never live here.
package launcher

import (
	"fmt"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
)

// ANSI colors, matching the shell scripts output style. Shared by
// every command that prints status output (secrets, migrate).
const (
	colorRed   = "\033[0;31m"
	colorGreen = "\033[0;32m"
	colorCyan  = "\033[0;36m"
	colorBold  = "\033[1m"
	colorReset = "\033[0m"
)

// versionVars builds the kong interpolation vars: the --version
// flag (kong.VersionFlag) prints the "version" variable verbatim.
func versionVars() kong.Vars {
	return kong.Vars{
		"version": fmt.Sprintf("%s %s %s (%s %s)", config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate),
	}
}

// CLI is the command-line grammar. Subcommand Run methods receive
// the parsed CLI via kong's type-based binding (ctx.Run(cli)).
type CLI struct {
	// EnvFile loads a dotenv file into the config layering, below the system environment.
	EnvFile string `help:"Load environment variables from a dotenv file (system env takes precedence)"`

	// Version prints the "version" variable and exits.
	Version kong.VersionFlag `short:"V" help:"Show the application version"`

	Serve   ServeCmd   `cmd:"" help:"Start the application server"`
	Migrate MigrateCmd `cmd:"" help:"Database migration commands"`
	Health  HealthCmd  `cmd:"" help:"Check application health" aliases:"hc"`
}

// RunCLI parses args and runs the selected command. Kept separate
// from Execute so tests can exercise parsing without os.Exit.
func RunCLI(args []string, opts ...kong.Option) error {
	cli := &CLI{}
	base := []kong.Option{
		kong.Name("tango"),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.UsageOnError(),
		versionVars(),
	}
	// Build-specific commands: secrets exist in debug builds
	// only; the migrate command surface is wired per-variant.
	base = append(base, secretsOptions()...)
	parser, err := kong.New(cli, append(base, opts...)...)
	if err != nil {
		return err
	}
	kctx, err := parser.Parse(args)
	if err != nil {
		return err
	}
	return kctx.Run(cli)
}
