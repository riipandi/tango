package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeEnvFile writes a dotenv file for the env-file layer tests.
func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), ".env.test")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, "localhost", cfg.Host)
	assert.Equal(t, 3080, cfg.Port)
	assert.Equal(t, "development", cfg.App.Mode)
	assert.Equal(t, "structured", cfg.App.LogFormat)
	assert.Equal(t, "http://localhost:3000", cfg.Public.BaseURL)
	assert.NotEmpty(t, cfg.Database.URL)
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("APP_MODE", "production")
	t.Setenv("PORT", "9999")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, "production", cfg.App.Mode)
	assert.Equal(t, 9999, cfg.Port)
}

func TestLoadEnvFileLayer(t *testing.T) {
	path := writeEnvFile(t, "PORT=1234\nAPP_LOG_LEVEL=debug\n")

	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 1234, cfg.Port)
	assert.Equal(t, "debug", cfg.App.LogLevel)
}

func TestLoadEnvFileDoesNotOverrideSystemEnv(t *testing.T) {
	t.Setenv("PORT", "7777")
	path := writeEnvFile(t, "PORT=1234\n")

	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 7777, cfg.Port, "system env must win over env file")
	assert.Equal(t, "info", cfg.App.LogLevel)
}

func TestLoadEnvFileMissing(t *testing.T) {
	_, err := Load(LoadOptions{EnvFile: "/nonexistent/tango/.env"})
	require.ErrorContains(t, err, "load env file")
}

func TestLoadOverridesWinOverEverything(t *testing.T) {
	t.Setenv("HOST", "from-env")
	path := writeEnvFile(t, "PORT=1234\n")

	cfg, err := Load(LoadOptions{
		EnvFile: path,
		Overrides: map[string]any{
			"host": "0.0.0.0",
			"port": 4321,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "0.0.0.0", cfg.Host)
	assert.Equal(t, 4321, cfg.Port, "overrides must win over env file and env")
}

func TestLoadEmptyEnvIsUnset(t *testing.T) {
	t.Setenv("APP_MODE", "")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, "development", cfg.App.Mode, "empty env must not shadow the default")
}

func TestLoadTrustedOrigins(t *testing.T) {
	t.Setenv("PUBLIC_TRUSTED_ORIGINS", "http://a.test,http://b.test")

	cfg, err := Load(LoadOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"http://a.test", "http://b.test"}, cfg.Public.TrustedOrigins)
}

func TestLoadNullPathPrefix(t *testing.T) {
	cfg, err := Load(LoadOptions{Overrides: map[string]any{
		"storage.s3.path_prefix": "null",
	}})
	require.NoError(t, err)
	assert.Nil(t, cfg.Storage.S3.PathPrefix, "literal null must decode to nil")

	cfg, err = Load(LoadOptions{Overrides: map[string]any{
		"storage.s3.path_prefix": "tenant-a",
	}})
	require.NoError(t, err)
	require.NotNil(t, cfg.Storage.S3.PathPrefix)
	assert.Equal(t, "tenant-a", *cfg.Storage.S3.PathPrefix)
}
