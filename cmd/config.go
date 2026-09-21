package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/envfile"
	"github.com/riipandi/tango/pkg/printext"
)

// ErrDatabaseURLUnset is returned when no DSN is available. It names the
// variable and the way to supply it, because the message is what a user reads
// when a command refuses to run.
var ErrDatabaseURLUnset = fmt.Errorf("%s is not set; set it in the config file or the environment",
	envfile.DatabaseURL)

// # Resolving the configuration

// configState holds what resolving the configuration produced: the result, and
// the failure when there is none.
//
// Resolution is deferred rather than returned from the root Before. The config
// file is required, and a broken one must not stop a command that bootstraps it:
// `config:generate` has to run before a file exists, and `key:generate` has to
// run before a referenced variable does. The failure is kept and reported by the
// command that actually needs the configuration.
type configState struct {
	cfg config.Config
	err error
}

// configFrom returns the configuration resolved for this run. A command that
// needs one key uses this, so a key it never reads cannot block it.
func configFrom(ctx context.Context) (config.Config, error) {
	state, ok := ctx.Value(configKey{}).(configState)
	if !ok {
		return config.Config{}, errors.New("config: not resolved for this command")
	}
	return state.cfg, state.err
}

// fullConfigFrom returns the configuration after checking it as a whole.
//
// A command that uses many keys calls this instead of configFrom: it turns a
// broken configuration into one error before the command starts, rather than a
// failure at whichever key it happens to reach first.
func fullConfigFrom(ctx context.Context) (config.Config, error) {
	cfg, err := configFrom(ctx)
	if err != nil {
		return config.Config{}, err
	}
	if err := cfg.Validate(); err != nil {
		return config.Config{}, err
	}
	return cfg, nil
}

// initConfig resolves the configuration once, before any subcommand runs, and
// puts it on the context. It runs on the root command, so its result is
// available to every action in the chain.
//
// A failure is stored, not returned. The command that needs the configuration
// reports it; a command that bootstraps the file runs without it.
func initConfig(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	cfg, err := config.Resolve(cmd)
	return context.WithValue(ctx, configKey{}, configState{cfg: cfg, err: err}), nil
}

// openStore opens the pool a command works through. The caller owns the handle
// and must close it.
//
// Every pool setting comes from cfg, so a command cannot open a pool that
// disagrees with the configuration.
func openStore(ctx context.Context, cfg config.Config) (*datastore.Postgres, error) {
	store, err := datastore.NewPostgres(ctx, datastore.PostgresOptions{
		DSN:             cfg.Database.URL,
		ApplicationName: config.AppIdentifier,
		MaxConns:        cfg.Database.MaxConns,
		MinConns:        cfg.Database.MinConns,
		MaxConnLifetime: cfg.Database.MaxConnLifetime,
		MaxConnIdleTime: cfg.Database.MaxConnIdleTime,
		ConnectTimeout:  cfg.Database.ConnectTimeout,
		SearchPath:      cfg.Database.SearchPath,
		Timezone:        cfg.Database.Timezone,
	})
	if err != nil {
		return nil, err
	}
	return store, nil
}

// requireDatabaseURL reports the missing DSN with the message every command
// uses, so a caller that only needs the connection string does not repeat it.
func requireDatabaseURL(cfg config.Config) (string, error) {
	if cfg.Database.URL == "" {
		return "", ErrDatabaseURLUnset
	}
	return cfg.Database.URL, nil
}

// dataDir resolves the application data directory from the configuration. The
// layer already applied the precedence, so there is nothing to re-check here.
func dataDir(cfg config.Config) string {
	if cfg.App.DataDir == "" {
		return config.DefaultDataDir
	}
	return cfg.App.DataDir
}

// configPath reports the file the configuration came from, so a command can name
// it. It is the path the layer resolved, not a second guess at it.
func configPath(cmd *cli.Command) string {
	return config.ConfigPath(
		config.Options{ConfigFile: cmd.String(config.FlagConfigFile)}, os.Environ())
}

// configKey is the context key under which the resolved configuration is stored.
// It is a private struct type so no other package can collide with it.
type configKey struct{}

// # The config commands

// configGenerateCmd writes the file every other command reads. It is the first
// thing a fresh checkout runs, so it needs no configuration of its own.
var configGenerateCmd = &cli.Command{
	Name:  "config:generate",
	Usage: "Write a starter config file",
	Description: `Writes a config file listing every configuration key with its default,
so a fresh checkout has one to edit.

Each secret is written as an env: directive naming the variable that holds it,
never as a literal, so the file is safe to keep. Generate the values with
key:generate, then set DATABASE_URL.

The file is not overwritten. Pass --overwrite to replace it, or --output to
write somewhere else.`,
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "output",
			Usage: "Write to this path instead of the default config file",
		},
		&cli.BoolFlag{
			Name:  "overwrite",
			Usage: "Replace an existing file",
		},
	},
	Action: runConfigGenerate,
}

// configFileMode is the permission of a generated config file. It is readable
// because the file holds directives, never a secret value.
const configFileMode fs.FileMode = 0o644

// runConfigGenerate writes the sample config and reports where it landed.
func runConfigGenerate(_ context.Context, cmd *cli.Command) error {
	p := printext.NewPalette(cmd.Root().Writer)

	path := cmd.String("output")
	if path == "" {
		path = configPath(cmd)
	}

	if err := mayWriteConfig(path, cmd.Bool("overwrite")); err != nil {
		return err
	}

	sample, err := config.Sample()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, sample, configFileMode); err != nil {
		return fmt.Errorf("config: write %s: %w", path, err)
	}
	return printConfigWritten(p, path)
}

// mayWriteConfig refuses to replace an existing file unless --overwrite was
// passed. It is a guard rather than a prompt: a config file is edited by hand,
// so losing it silently is worse than being told to pass a flag.
func mayWriteConfig(path string, overwrite bool) error {
	_, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		return fmt.Errorf("config: inspect %s: %w", path, err)
	case overwrite:
		return nil
	default:
		return fmt.Errorf("%s already exists; pass --overwrite to replace it", path)
	}
}

// printConfigWritten reports the file and the step that follows it, as the same
// labelled block every other command prints.
//
// The key count and the size are left out: a generated file always carries every
// key, so both numbers are the same on every run and say nothing about this one.
func printConfigWritten(p printext.Palette, path string) error {
	if err := printFields(p, []field{{"written", path}}); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(p.Writer(), "\nnext: run %s, then set %s\n",
		p.Dim("key:generate --env-file=.env.local"), p.Dim(config.EnvName("database.url"))); err != nil {
		return err
	}
	return printStatusLine(p, "config file ready")
}

var configValidateCmd = &cli.Command{
	Name:  "config:validate",
	Usage: "Check the configuration",
	Description: `Checks the resolved configuration and reports every problem it finds, not
just the first, so a config file with several mistakes is fixed in one pass.

It resolves the same way a running command does: the config file, the
environment its directives reference, and the command-line flags. Pass
--config-file to check a file somewhere else.

Nothing is written, so it is safe to run in CI.`,
	Action: runConfigValidate,
}

// runConfigValidate checks the whole configuration and reports each problem.
func runConfigValidate(ctx context.Context, cmd *cli.Command) error {
	p := printext.NewPalette(cmd.Root().Writer)
	started := time.Now()

	cfg, err := fullConfigFrom(ctx)
	if err != nil {
		return err
	}
	return printConfigValid(p, configPath(cmd), cfg, time.Since(started))
}

// printConfigValid reports the file that was checked, the resolved database
// target, and how many keys resolved.
func printConfigValid(p printext.Palette, path string, cfg config.Config, elapsed time.Duration) error {
	keys := config.Keys()
	if err := p.Printf("%s\n", p.Dim(path)); err != nil {
		return err
	}
	if cfg.Database.URL != "" {
		if err := p.Printf("%s\n", p.Dim("database: "+config.RedactDSN(cfg.Database.URL))); err != nil {
			return err
		}
	}
	return printStatusLine(p, "%s %s",
		p.Green(fmt.Sprintf("%d %s", len(keys), printext.Plural(len(keys), "key"))),
		p.Dim("valid in "+printext.Duration(elapsed)))
}
