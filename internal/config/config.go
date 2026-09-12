// Package config loads runtime config by layering, later wins:
//
//	defaults → system env → --env-file → overrides
//
// --env-file wins over system env; empty values are unset and never
// shadow defaults. Unknown keys fail in the env file (curated) but
// are ignored in system env (shared namespace).
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
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// validKeys is every dotted key from Config's koanf tags; adding a
// field adds its key automatically.
var validKeys = buildKeySet(reflect.TypeFor[Config](), "")

// buildKeySet collects dotted keys of koanf-tagged leaves,
// recursing into nested structs with the tag as prefix.
func buildKeySet(t reflect.Type, prefix string) map[string]bool {
	keys := make(map[string]bool)
	for i := range t.NumField() {
		field := t.Field(i)

		name := field.Tag.Get("koanf")
		if name == "" || name == "-" {
			continue
		}
		path := name
		if prefix != "" {
			path = prefix + "." + name
		}

		if field.Type.Kind() == reflect.Struct {
			for key, ok := range buildKeySet(field.Type, path) {
				keys[key] = ok
			}
			continue
		}
		keys[path] = true
	}
	return keys
}

// LoadOptions parametrizes Load.
type LoadOptions struct {
	// EnvFile is an optional dotenv file between defaults and env.
	EnvFile string

	// Overrides win over everything (resolved CLI flags).
	Overrides map[string]any
}

// Load assembles layered config and decodes it into Config.
func Load(opts LoadOptions) (*Config, error) {
	k := koanf.New(".")

	// Defaults from the Config type itself.
	if err := k.Load(structs.Provider(defaultConfig, "koanf"), nil); err != nil {
		return nil, fmt.Errorf("load defaults: %w", err)
	}

	// System env, below the env file.
	if layer, err := envLayer(); err != nil {
		return nil, err
	} else if len(layer) > 0 {
		if err := k.Load(confmap.Provider(layer, "."), nil); err != nil {
			return nil, fmt.Errorf("load environment: %w", err)
		}
	}

	// Optional --env-file, wins over system env.
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

	// Explicit overrides (CLI flags).
	for key, value := range opts.Overrides {
		if err := k.Set(key, value); err != nil {
			return nil, fmt.Errorf("override %s: %w", key, err)
		}
	}

	var cfg Config
	if err := k.UnmarshalWithConf("", &cfg, unmarshalConf()); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

// envLayer maps bound system env vars; unknown keys ignored
// (shared namespace carries foreign vars).
func envLayer() (map[string]any, error) {
	layer := make(map[string]any)
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		key, mapped := envTransform(name, value)
		if key == "" {
			continue
		}
		layer[key] = mapped
	}
	return layer, nil
}

// envSections maps env prefixes to key sections.
// HOST and PORT are unprefixed.
var envSections = []struct{ prefix, section string }{
	{"APP_", "app"},
	{"AUTH_", "auth"},
	{"DATABASE_", "database"},
	{"MAILER_", "mailer"},
	{"PUBLIC_", "public"},
	{"STORAGE_", "storage"},
}

// envTransform maps an env var to its key: first segment is the
// section, rest keeps snake_case (APP_LOG_LEVEL -> app.log_level).
// Unbound vars return ""; empty values are unset.
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

// envFileLayer reads a dotenv file through envTransform. Unknown
// keys fail: the file is curated, mismatch is a defect.
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
		key, mapped := envTransform(name, fmt.Sprintf("%v", value))
		if key == "" {
			continue
		}
		if !validKeys[key] {
			return nil, fmt.Errorf("load env file %s: unknown config key from %q", path, name)
		}
		layer[key] = mapped
	}
	return layer, nil
}

// unmarshalConf decodes into Config: weak typing (env strings to
// ints/bools), comma slices, "null" to nil *string.
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
