// Package config resolves the application configuration from every source the
// application supports, in one fixed precedence order.
//
// # Sources and precedence
//
// From lowest to highest:
//
//  1. the built-in defaults (Default)
//  2. a JSON config file (--config-file, CONFIG_FILE, or app.config.json)
//  3. the system environment
//  4. an env file (--env-file)
//  5. the command-line flags
//
// The env file beats the system environment, and a flag beats both. Merge is
// last-wins per key, not per section: a source that sets one leaf leaves its
// siblings alone.
//
// # The config file
//
// A config file is optional. When no source names one, app.config.json in the
// working directory is used if it exists, and its absence is not an error: a
// fresh checkout has none and must still run. A file the user named is required,
// so a typo in --config-file is reported instead of silently ignored.
//
// # Key names
//
// A key is a dotted path, such as database.max_conns. The same key is spelled
// DATABASE_MAX_CONNS in the environment, because the mapping is derived from the
// struct and applied in both directions (see EnvName).
//
// # The struct is the schema
//
// Config and its sections define every key, its default (Default), and its rule
// (Validate). Adding a key means adding a field with a koanf and a json tag, a
// default, and a rule; the key then works in every source at once.
//
// # Conflict resolution is observable
//
// A resolved Config records which source last set each key, readable through
// Origin. That is how a caller tells an env-file value from a flag value without
// re-deriving the precedence.
package config

import (
	"fmt"
	"maps"
	"os"

	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/v2"
)

// Options describes the sources Load merges. Every field is optional: with no
// options the result is Default().
type Options struct {
	// ConfigFile is the path to a JSON config file. When empty, FileEnv is
	// consulted, and DefaultConfigFile is tried last.
	ConfigFile string
	// EnvFile holds the key-value pairs of a dotenv file, already parsed. It
	// wins over the system environment.
	EnvFile map[string]string
	// Flags holds the command-line values to apply. It wins over every other
	// source. Keys are config paths, not flag names.
	Flags map[string]any
	// Environ is the system environment to read, in NAME=value form. It
	// defaults to os.Environ; a test passes a fixed slice.
	Environ []string
}

// Load resolves the configuration from every source, in the documented
// precedence order: built-in defaults, then the JSON config file, then the
// system environment, then the env file, then the command-line flags.
//
// Each layer is flattened before it is merged, so a nested object in the file
// and a flat environment variable override the same key.
//
// Only known keys are accepted. A key that is not part of Config is dropped, so
// a stray variable naming a section cannot replace that section with a scalar
// and break the decode.
//
// Load does not validate: a command that needs one key, such as a migration that
// needs only database.url, must not be blocked by a key it never reads. Call
// Validate where the whole configuration is required.
func Load(opts Options) (Config, error) {
	environ := opts.Environ
	if environ == nil {
		environ = os.Environ()
	}

	cfg := Default()
	cfg.origin = make(map[string]string)
	k := koanf.New(Delim)

	fileKeys, err := configFileLayer(opts, environ)
	if err != nil {
		return Config{}, err
	}

	layers := []struct {
		name string
		keys map[string]any
	}{
		{LayerDefault, DefaultsMap()},
		{LayerConfigFile, fileKeys},
		{LayerSystemEnv, envLayer(environ)},
		{LayerEnvFile, envFileLayer(opts.EnvFile)},
		{LayerFlag, filterKnown(opts.Flags)},
	}
	for _, layer := range layers {
		if err := merge(k, cfg.origin, layer.name, layer.keys); err != nil {
			return Config{}, err
		}
	}

	// Unmarshal into the defaults, so a key no source set keeps its default.
	if err := k.Unmarshal("", &cfg); err != nil {
		return Config{}, fmt.Errorf("config: decode: %w", err)
	}
	return cfg, nil
}

// merge applies one layer to the accumulated configuration and records it as the
// origin of the keys it set, replacing an earlier entry. The last layer to set a
// key is the one that won.
func merge(k *koanf.Koanf, origin map[string]string, layer string, keys map[string]any) error {
	if len(keys) == 0 {
		return nil
	}
	if err := k.Load(confmap.Provider(keys, Delim), nil); err != nil {
		return fmt.Errorf("config: load %s: %w", layer, err)
	}
	for key := range keys {
		origin[key] = layer
	}
	return nil
}

// Origin reports which source last set key, or an empty string when no source
// set it. The names are LayerDefault, LayerConfigFile, LayerSystemEnv,
// LayerEnvFile, and LayerFlag.
//
// It makes precedence observable: a key set by both the environment and the env
// file reports LayerEnvFile, and one also set on the command line reports
// LayerFlag.
func (c Config) Origin(key string) string {
	return c.origin[key]
}

// Origins returns a copy of the source of every resolved key.
func (c Config) Origins() map[string]string {
	out := make(map[string]string, len(c.origin))
	maps.Copy(out, c.origin)
	return out
}
