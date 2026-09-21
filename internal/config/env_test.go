package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

func TestEnvFileAndEnvironmentUseTheSameNames(t *testing.T) {
	// One name works whether the variable is exported or written to a file, so
	// a deployment and a local run cannot disagree about how a key is spelled.
	cfg, err := config.Load(config.Options{
		Environ: baseEnv(),
		EnvFile: map[string]string{
			"SERVER_PORT":     "2222",
			"DATABASE_URL":    dsn,
			"AUTH_SECRET_KEY": secret,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, 2222, cfg.Server.Port)
	assert.Equal(t, config.LayerEnvFile, cfg.Origin("server.port"))
}

func TestEnvNameRoundTripsEveryKey(t *testing.T) {
	// Every key must be reachable from the environment, and the name must be
	// unique: two keys sharing a name would make one unreachable.
	seen := make(map[string]string)
	for _, key := range config.Keys() {
		name := config.EnvName(key)
		assert.NotContains(t, seen, name, "duplicate environment name for %s and %s", seen[name], key)
		seen[name] = key
	}
	assert.Equal(t, "DATABASE_URL", config.EnvName("database.url"))
	assert.Equal(t, "AUTH_SECRET_KEY", config.EnvName("auth.secret_key"))
	assert.Equal(t, "SERVER_READ_TIMEOUT", config.EnvName("server.read_timeout"))
}

func TestUnknownEnvironmentVariableIsIgnored(t *testing.T) {
	// A variable naming no config key must not reach the layer: an unrelated
	// variable in the process environment cannot change the configuration.
	environ := append(baseEnv(), "NOPE=9999", "HOSTNAME=some-host")

	cfg, err := config.Load(config.Options{Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, config.Default().Server.Port, cfg.Server.Port)
	assert.Equal(t, config.Default().Server.Host, cfg.Server.Host)
	assert.Empty(t, cfg.Origin("nope"))
}

func TestUnknownEnvFileVariableIsIgnored(t *testing.T) {
	cfg, err := config.Load(config.Options{
		Environ: baseEnv(),
		EnvFile: map[string]string{"NOPE": "9999", "STORAGE": "oops"},
	})
	require.NoError(t, err)

	assert.Equal(t, config.StorageLocal, cfg.Storage.Driver)
	assert.Equal(t, config.DefaultDataDir, cfg.Storage.LocalPath)
}

func TestVariableNamingASectionIsIgnored(t *testing.T) {
	// A stray variable whose name is a section, not a key, must not replace the
	// section with a scalar: the keys inside it must survive.
	environ := append(baseEnv(), "STORAGE=oops", "SERVER=oops")

	cfg, err := config.Load(config.Options{Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, config.StorageLocal, cfg.Storage.Driver)
	assert.Equal(t, config.Default().Server.Host, cfg.Server.Host)
}

func TestEmptyValueIsKept(t *testing.T) {
	// An empty value is a decision, unlike an absent variable: it must reach the
	// layer so validation can report it.
	cfg, err := config.Load(config.Options{
		Environ: append(baseEnv(), "SERVER_BASE_URL="),
	})
	require.NoError(t, err)

	assert.Empty(t, cfg.Server.BaseURL)
	assert.Equal(t, config.LayerSystemEnv, cfg.Origin("server.base_url"))
}

func TestDurationFromEnvironment(t *testing.T) {
	environ := append(baseEnv(), "SERVER_READ_TIMEOUT=45s")

	cfg, err := config.Load(config.Options{Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, 45*time.Second, cfg.Server.ReadTimeout)
}

func TestNumericFromEnvironment(t *testing.T) {
	environ := append(baseEnv(), "SERVER_PORT=9000", "DATABASE_MAX_CONNS=25")

	cfg, err := config.Load(config.Options{Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, 9000, cfg.Server.Port)
	assert.Equal(t, int32(25), cfg.Database.MaxConns)
}
