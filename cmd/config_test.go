package main

import (
	"bytes"
	"context"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
)

// testRoot builds the root command a test runs a subcommand through.
//
// It mirrors the real root command: the config flags are registered, and
// initConfig runs as Before, so a test exercises the same precedence chain the
// binary does. Only the flag names differ from cli_debug.go, which registers
// exactly these two.
//
// A test that needs a value supplies it through --env-file, the environment, or
// a config file, because there is no --data-dir flag any more: the data
// directory comes from the configuration alone.
func testRoot(out *bytes.Buffer, stdin string, commands ...*cli.Command) *cli.Command {
	return &cli.Command{
		Name:     "tango",
		Writer:   out,
		Reader:   strings.NewReader(stdin),
		Commands: commands,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: config.FlagEnvFile, Usage: "Load environment variables from a file"},
			&cli.StringFlag{Name: config.FlagConfigFile, Usage: "Load configuration from a JSON file"},
		},
		// cli.Exit calls os.Exit, so the error is trapped instead.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Before:         initConfig,
	}
}
