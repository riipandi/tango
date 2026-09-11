// Package launcher implements the tango command line.
//
// The command surface is declared once as a struct (kong); runtime
// configuration is layered separately by internal/config (koanf).
// Kong flags stay zero-valued — config defaults never live here.
package launcher

import (
	"fmt"
	"os"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
)

// VersionCmd shows the application version.
type VersionCmd struct {
	Short    bool `short:"s" help:"Show short version"`
	Semantic bool `short:"S" help:"Show semantic version"`
}

// Run prints the version.
func (v *VersionCmd) Run() error {
	switch {
	case v.Short:
		fmt.Printf("%s (%s)\n", config.AppVersion, config.BuildHash)
	case v.Semantic:
		fmt.Printf("%s\n", config.AppVersion)
	default:
		fmt.Printf("%s %s %s (%s %s)\n", config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate)
	}
	return nil
}

// CLI is the command-line grammar. Subcommand Run methods receive
// the parsed CLI via kong's type-based binding (ctx.Run(cli)).
type CLI struct {
	// EnvFile loads a dotenv file into the config layering, below the system environment.
	EnvFile string `help:"Load environment variables from a dotenv file (system env takes precedence)"`

	Version VersionCmd `cmd:"" help:"Show the application version"`
	Serve   ServeCmd   `cmd:"" help:"Start the application server"`
	Migrate MigrateCmd `cmd:"" help:"Database migration commands"`
	Health  HealthCmd  `cmd:"" help:"Check application health" aliases:"hc"`

	kong.Plugins
}

// newCLI returns the grammar with build-specific plugins. The
// secrets command exists only in debug builds.
func newCLI() *CLI {
	cli := &CLI{}
	cli.Plugins = secretsPlugins()
	return cli
}

// Execute parses os.Args and runs the selected command.
func Execute() {
	if err := runCLI(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runCLI parses args and runs the selected command. Kept separate
// from Execute so tests can exercise parsing without os.Exit.
func runCLI(args []string, opts ...kong.Option) error {
	cli := newCLI()
	base := []kong.Option{
		kong.Name("tango"),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.UsageOnError(),
	}
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
