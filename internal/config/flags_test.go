package config_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
)

// runCommand builds the root command the CLI uses, runs it with args, and
// returns what FromCommand saw inside the action. The action runs at the deepest
// command, which is the point in the chain the real actions run at.
func runCommand(t *testing.T, args []string) config.Options {
	t.Helper()

	var captured config.Options
	capture := func(_ context.Context, cmd *cli.Command) error {
		opts, err := config.FromCommand(cmd)
		if err != nil {
			return err
		}
		captured = opts
		return nil
	}

	root := &cli.Command{
		Name: "tango",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: config.FlagConfigFile},
			&cli.StringFlag{Name: config.FlagEnvFile},
			&cli.StringFlag{Name: config.FlagDataDir, Value: config.DefaultDataDir},
		},
		Commands: []*cli.Command{{
			Name: "serve",
			Flags: []cli.Flag{
				&cli.StringFlag{Name: "host", Value: "0.0.0.0"},
				&cli.UintFlag{Name: "port", Value: 3080},
				&cli.StringFlag{Name: "base-url"},
			},
			Action: capture,
		}},
	}
	require.NoError(t, root.Run(t.Context(), append([]string{"tango"}, args...)))
	return captured
}

func TestFromCommandReadsTheRootFlagFromASubcommand(t *testing.T) {
	// --data-dir is declared on the root command but the action runs on the
	// subcommand. The flag must still reach the config layer.
	opts := runCommand(t, []string{"--data-dir=/tmp/data", "serve"})

	require.Contains(t, opts.Flags, "app.data_dir")
	assert.Equal(t, "/tmp/data", opts.Flags["app.data_dir"])
}

func TestFromCommandReadsSubcommandFlags(t *testing.T) {
	opts := runCommand(t, []string{"serve", "--host=1.2.3.4", "--port=9000", "--base-url=https://x.test"})

	assert.Equal(t, "1.2.3.4", opts.Flags["server.host"])
	assert.EqualValues(t, 9000, opts.Flags["server.port"])
	assert.Equal(t, "https://x.test", opts.Flags["server.base_url"])
}

func TestFromCommandOmitsUnsetFlags(t *testing.T) {
	// A flag left at its default is not a decision the user made: it must not
	// reach the layer, or it would override a config file with a default.
	opts := runCommand(t, []string{"serve"})

	assert.NotContains(t, opts.Flags, "server.port")
	assert.NotContains(t, opts.Flags, "server.host")
	assert.NotContains(t, opts.Flags, "server.base_url")
	assert.NotContains(t, opts.Flags, "app.data_dir")
}

func TestFromCommandReadsConfigFileFlag(t *testing.T) {
	opts := runCommand(t, []string{"--config-file=/tmp/app.json", "serve"})

	assert.Equal(t, "/tmp/app.json", opts.ConfigFile)
}

func TestFromCommandReadsEnvFileFlag(t *testing.T) {
	path := writeEnvFile(t, "SERVER_PORT=4321\nNOPE=1\n")

	opts := runCommand(t, []string{"--env-file=" + path, "serve"})

	require.Contains(t, opts.EnvFile, "SERVER_PORT")
	assert.Equal(t, "4321", opts.EnvFile["SERVER_PORT"])
	assert.Contains(t, opts.EnvFile, "NOPE", "the env file is read whole; the loader drops unknown names")
}

func TestFromCommandWithoutEnvFileIsEmpty(t *testing.T) {
	opts := runCommand(t, []string{"serve"})

	assert.Empty(t, opts.ConfigFile)
	assert.Nil(t, opts.EnvFile)
}

func TestResolveAppliesFlagOverEverything(t *testing.T) {
	// The full chain through the CLI: a config file sets the port, the
	// environment overrides it, and the flag overrides both.
	path := writeConfig(t, `{"server": {"port": 1111}}`)

	var cfg config.Config
	root := &cli.Command{
		Name: "tango",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: config.FlagConfigFile},
			&cli.StringFlag{Name: config.FlagDataDir, Value: config.DefaultDataDir},
		},
		Commands: []*cli.Command{{
			Name:  "serve",
			Flags: []cli.Flag{&cli.UintFlag{Name: "port", Value: 3080}},
			Action: func(_ context.Context, cmd *cli.Command) error {
				var err error
				cfg, err = config.Resolve(cmd)
				return err
			},
		}},
	}

	t.Setenv("SERVER_PORT", "2222")
	err := root.Run(t.Context(), []string{"tango", "--config-file=" + path, "serve", "--port=3333"})
	require.NoError(t, err)

	assert.Equal(t, 3333, cfg.Server.Port)
	assert.Equal(t, config.LayerFlag, cfg.Origin("server.port"))
}
