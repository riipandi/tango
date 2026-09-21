package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/envfile"
)

// ErrDatabaseURLUnset is returned when no DSN is available. It names the
// variable and the way to supply it, because the message is what a user reads
// when a command refuses to run.
var ErrDatabaseURLUnset = fmt.Errorf("%s is not set; pass --env-file or export it", envfile.DatabaseURL)

// configFrom returns the configuration resolved for this run, as merged. It does
// not check the configuration as a whole: a command that needs one key uses this,
// so a key it never reads cannot block it.
func configFrom(ctx context.Context) (config.Config, error) {
	cfg, ok := ctx.Value(configKey{}).(config.Config)
	if !ok {
		return config.Config{}, errors.New("config: not resolved for this command")
	}
	return cfg, nil
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

// initConfig resolves the configuration once, before any subcommand runs, and
// puts it on the context. It runs on the root command, so its result is
// available to every action in the chain.
//
// Resolution does not validate. A command that needs one key must not be blocked
// by a key it never reads, so a command that needs the whole configuration asks
// for it with fullConfigFrom.
func initConfig(ctx context.Context, cmd *cli.Command) (context.Context, error) {
	cfg, err := config.Resolve(cmd)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, configKey{}, cfg), nil
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

// configKey is the context key under which the resolved configuration is stored.
// It is a private struct type so no other package can collide with it.
type configKey struct{}
