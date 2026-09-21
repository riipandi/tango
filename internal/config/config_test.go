package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

// dsn is a valid Postgres connection string, so a test that only exercises
// precedence does not fail validation for an unrelated reason.
const dsn = "postgresql://user:pass@localhost:5432/tango?sslmode=disable"

// secret is a 64-character hex string, the shape of APP_SECRET_KEY.
const secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// writeConfig writes a JSON config file and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

// baseEnv is the environment every test starts from: just enough to satisfy
// validation, with no key any test asserts on.
func baseEnv() []string {
	return []string{
		"DATABASE_URL=" + dsn,
		"AUTH_SECRET_KEY=" + secret,
	}
}

func TestDefaultsAreValid(t *testing.T) {
	cfg, err := config.Load(config.Options{Environ: baseEnv()})
	require.NoError(t, err)

	defaults := config.Default()
	assert.Equal(t, defaults.Server.Port, cfg.Server.Port)
	assert.Equal(t, defaults.App.DataDir, cfg.App.DataDir)
	assert.Equal(t, defaults.Log.Level, cfg.Log.Level)
	assert.Equal(t, config.LayerDefault, cfg.Origin("server.port"))
}

func TestLoadWithoutAnySourceUsesDefaults(t *testing.T) {
	// No environment, no file, no flags: every key must still resolve, so the
	// merge cannot depend on a source being present.
	cfg, err := config.Load(config.Options{Environ: baseEnv()})
	require.NoError(t, err)
	assert.Equal(t, config.Default().Server, cfg.Server)

	// A file that was named must exist, even when nothing else is set.
	_, err = config.Load(config.Options{
		Environ:    baseEnv(),
		ConfigFile: filepath.Join(t.TempDir(), "none.json"),
	})
	require.ErrorIs(t, err, config.ErrNoConfigFile)
}

// writeEnvFile writes a dotenv file and returns its path.
func writeEnvFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env.local")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
