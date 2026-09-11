package config

import (
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// freshViper rebuilds the package viper instance: viper.Set overrides
// are sticky, so every test starts from defaults + env bindings.
func freshViper(t *testing.T) {
	t.Helper()
	v = viper.New()
	setDefaults()
	bindEnvVars()
	v.AutomaticEnv()
}

func TestDefaults(t *testing.T) {
	freshViper(t)

	assert.Equal(t, "localhost", V().GetString("host"))
	assert.Equal(t, 3080, V().GetInt("port"))
	assert.Equal(t, "development", V().GetString("app.mode"))
	assert.Equal(t, "http://localhost:3000", V().GetString("public.base_url"))
}

func TestLoad(t *testing.T) {
	freshViper(t)

	cfg, err := Load()
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 3080, cfg.Port)
	assert.NotEmpty(t, cfg.Database.URL)
	assert.Same(t, cfg, C, "Load must update the package-level C")
}

func TestApplyFlags(t *testing.T) {
	freshViper(t)

	ApplyFlags("127.0.0.1", "9999")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", cfg.Host)
	assert.Equal(t, 9999, cfg.Port)
}

func TestApplyFlagsIgnoresInvalidPort(t *testing.T) {
	freshViper(t)

	ApplyFlags("127.0.0.1", "9999")
	ApplyFlags("", "not-a-port")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, 9999, cfg.Port, "invalid port must be ignored")
}

func TestApplyFlagsRespectsEnv(t *testing.T) {
	freshViper(t)
	t.Setenv("HOST", "from-env")
	t.Setenv("PORT", "7777")

	// Empty flags must not override env vars.
	ApplyFlags("ignored", "1234")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Equal(t, "from-env", cfg.Host)
	assert.Equal(t, 7777, cfg.Port)
}

func TestNullStringHook(t *testing.T) {
	freshViper(t)

	V().Set("storage.s3.path_prefix", "null")

	cfg, err := Load()
	require.NoError(t, err)
	assert.Nil(t, cfg.Storage.S3.PathPrefix, "literal null must decode to nil")

	V().Set("storage.s3.path_prefix", "tenant-a")
	cfg, err = Load()
	require.NoError(t, err)
	require.NotNil(t, cfg.Storage.S3.PathPrefix)
	assert.Equal(t, "tenant-a", *cfg.Storage.S3.PathPrefix)
}
