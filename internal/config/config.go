// Package config loads the runtime configuration by explicit
// layering, where later layers win:
//
//	defaults → --env-file (dotenv) → system environment → overrides
//
// There is no implicit loading and no config-file format to
// negotiate — the whole pipeline is visible in Load. The system
// environment always wins over the env file, and empty environment
// variables are treated as unset.
package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

// LoadOptions parametrizes Load.
type LoadOptions struct {
	// EnvFile is an optional dotenv file layered between the
	// defaults and the system environment.
	EnvFile string

	// Overrides are explicit values applied on top of everything
	// else (resolved CLI flags). Values are keyed by config key.
	Overrides map[string]any
}

// Load assembles the layered configuration and decodes it into the
// typed Config struct.
func Load(opts LoadOptions) (*Config, error) {
	k := koanf.New(".")

	if err := k.Load(confmap.Provider(defaultsMap(), "."), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	if opts.EnvFile != "" {
		layer, err := envFileLayer(opts.EnvFile)
		if err != nil {
			return nil, err
		}
		if len(layer) > 0 {
			if err := k.Load(confmap.Provider(layer, "."), nil); err != nil {
				return nil, fmt.Errorf("load env file: %w", err)
			}
		}
	}

	if layer := envLayer(); len(layer) > 0 {
		if err := k.Load(confmap.Provider(layer, "."), nil); err != nil {
			return nil, fmt.Errorf("load environment: %w", err)
		}
	}

	for key, value := range opts.Overrides {
		if err := k.Set(key, value); err != nil {
			return nil, fmt.Errorf("override %s: %w", key, err)
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, unmarshalConf()); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	return &cfg, nil
}

// envFileLayer reads a dotenv file and remaps its keys through the
// binding table. Only bound variables are accepted — unknown keys in
// the file are ignored — and the same value normalization applies
// as the environment layer.
func envFileLayer(path string) (map[string]any, error) {
	raw, err := file.Provider(path).ReadBytes()
	if err != nil {
		return nil, fmt.Errorf("load env file %s: %w", path, err)
	}

	parsed, err := dotenv.Parser().Unmarshal(raw)
	if err != nil {
		return nil, fmt.Errorf("parse env file %s: %w", path, err)
	}

	layer := make(map[string]any, len(parsed))
	for envName, value := range parsed {
		key, ok := envKeys[envName]
		if !ok {
			continue
		}
		layer[key] = expandValue(key, value)
	}
	return layer, nil
}

// envLayer maps bound system environment variables to their config
// keys. Unbound variables are ignored; empty values are treated as
// unset so they never shadow the defaults.
func envLayer() map[string]any {
	layer := make(map[string]any, len(envBindings))
	for key, envName := range envBindings {
		value, ok := os.LookupEnv(envName)
		if !ok || value == "" {
			continue
		}
		layer[key] = expandValue(key, value)
	}
	return layer
}

// expandValue applies key-specific value normalization: the only
// slice-typed key splits on commas.
func expandValue(key string, value any) any {
	if key == "public.trusted_origins" {
		if s, ok := value.(string); ok {
			return strings.Split(s, ",")
		}
	}
	return value
}

// envKeys is the reverse of envBindings: environment variable name
// → config key.
var envKeys = func() map[string]string {
	reversed := make(map[string]string, len(envBindings))
	for key, envName := range envBindings {
		reversed[envName] = key
	}
	return reversed
}()

// unmarshalConf decodes into Config: weakly typed (env strings to
// ints/bools), comma-separated slices, and a hook turning the
// literal "null" into a nil *string.
func unmarshalConf() koanf.UnmarshalConf {
	return koanf.UnmarshalConf{
		Tag: "koanf",
		DecoderConfig: &mapstructure.DecoderConfig{
			WeaklyTypedInput: true,
			DecodeHook: mapstructure.ComposeDecodeHookFunc(
				mapstructure.StringToSliceHookFunc(","),
				nullStringToNullableStringHook(),
			),
		},
	}
}

func nullStringToNullableStringHook() mapstructure.DecodeHookFuncType {
	return func(
		f reflect.Type,
		t reflect.Type,
		data any,
	) (any, error) {
		if t != reflect.TypeFor[*string]() {
			return data, nil
		}

		if s, ok := data.(string); ok && strings.ToLower(s) == "null" {
			return nil, nil
		}

		return data, nil
	}
}
