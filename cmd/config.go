package main

import (
	"context"
	"errors"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
)

// configKey is the context key under which the resolved configuration is stored
// for a command action. It is a private struct type so no other package can
// collide with it.
type configKey struct{}

// configFrom returns the configuration initConfig resolved for this run. A
// command that needs a config value reads it here rather than re-resolving the
// sources, so every command in a run sees the same merged result.
func configFrom(ctx context.Context) (config.Config, error) {
	cfg, ok := ctx.Value(configKey{}).(config.Config)
	if !ok {
		return config.Config{}, errors.New("config: not resolved for this command")
	}
	return cfg, nil
}

// initConfig resolves the configuration once, before any subcommand runs, and
// puts it on the context. It runs on the root command, so its result is
// available to every action in the chain.
//
// The precedence it applies is the documented one: built-in defaults, then the
// JSON config file, then the system environment, then the env file, then the
// flags the user set. The env file beats the environment, and a flag beats both.
//
// Resolution does not validate. A command that needs one key must not be blocked
// by a key it never reads, so validation belongs to the caller that needs the
// whole configuration.
func initConfig(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	cfg, err := config.Resolve(cmd)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, configKey{}, cfg), nil
}
