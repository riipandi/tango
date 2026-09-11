// Package config loads the runtime configuration by explicit
// layering, where later layers win:
//
//	defaults → --env-file (dotenv) → system environment → overrides
//
// The whole pipeline is visible in Load — there is no implicit
// loading. The system environment always wins over the env file,
// and empty values are treated as unset so they never shadow the
// defaults.
package config

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/dotenv"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/env/v2"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
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

	// 1. Defaults: the Config type itself (see defaultConfig).
	if err := k.Load(structs.Provider(defaultConfig, "koanf"), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	// 2. Optional --env-file, mapped through the same env rules.
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

	// 3. System environment — always wins over the env file.
	if err := k.Load(env.Provider(".", env.Opt{TransformFunc: envTransform}), nil); err != nil {
		return nil, fmt.Errorf("load environment: %w", err)
	}

	// 4. Explicit overrides (resolved CLI flags).
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

// envSections maps environment variable prefixes to config key
// sections. HOST and PORT are unprefixed.
var envSections = []struct{ prefix, section string }{
	{"APP_", "app"},
	{"AUTH_", "auth"},
	{"DATABASE_", "database"},
	{"MAILER_", "mailer"},
	{"PUBLIC_", "public"},
	{"STORAGE_", "storage"},
}

// envTransform maps a system environment variable to its config key:
// the first underscore-separated segment selects the section and the
// rest keeps its snake_case form (APP_LOG_LEVEL -> app.log_level,
// STORAGE_S3_REGION -> storage.s3_region). Unbound variables are
// skipped by returning an empty key; empty values are treated as
// unset so they never shadow the defaults.
func envTransform(key, value string) (string, any) {
	switch key {
	case "HOST":
		return "host", value
	case "PORT":
		return "port", value
	case "":
		return "", nil
	}
	for _, section := range envSections {
		if !strings.HasPrefix(key, section.prefix) {
			continue
		}
		if value == "" {
			return "", nil
		}
		rest := strings.ToLower(strings.TrimPrefix(key, section.prefix))
		return section.section + "." + rest, value
	}
	return "", nil
}

// envFileLayer reads a dotenv file and maps it through the same
// environment rules as envTransform. Unknown keys are ignored.
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
	for name, value := range parsed {
		if key, mapped := envTransform(name, fmt.Sprintf("%v", value)); key != "" {
			layer[key] = mapped
		}
	}
	return layer, nil
}

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
