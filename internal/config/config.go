package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"

	"github.com/mitchellh/mapstructure"
	"github.com/spf13/viper"
)

var (
	v        *viper.Viper
	C        *Config
	initOnce sync.Once
)

func Init() {
	initOnce.Do(func() {
		v = viper.New()
		setDefaults()
		bindEnvVars()
		v.AutomaticEnv()
	})
}

func V() *viper.Viper {
	Init()
	return v
}

func ApplyFlags(host string, port string) {
	if host != "" && os.Getenv("HOST") == "" {
		v.Set("host", host)
	}
	if port != "" && os.Getenv("PORT") == "" {
		cleaned := strings.TrimLeft(port, ":")
		var p int
		_, err := fmt.Sscanf(cleaned, "%d", &p)
		if err == nil {
			v.Set("port", p)
		}
	}
}

func Load() (*Config, error) {
	hook := mapstructure.ComposeDecodeHookFunc(
		nullStringToNullableStringHook(),
	)

	var cfg Config
	if err := v.Unmarshal(&cfg, viper.DecodeHook(hook)); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	C = &cfg
	return C, nil
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
