package config_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

// Precedence, lowest to highest: defaults, config file, system environment, env
// file, flags. The env file beats the environment, and a flag beats both.

func TestEnvFileBeatsSystemEnvironment(t *testing.T) {
	environ := append(baseEnv(), "SERVER_PORT=1111")

	cfg, err := config.Load(config.Options{
		Environ: environ,
		EnvFile: map[string]string{"SERVER_PORT": "2222"},
	})
	require.NoError(t, err)

	assert.Equal(t, 2222, cfg.Server.Port, "the env file must win over the system environment")
	assert.Equal(t, config.LayerEnvFile, cfg.Origin("server.port"))
}

func TestFlagBeatsEnvFileAndSystemEnvironment(t *testing.T) {
	environ := append(baseEnv(), "SERVER_PORT=1111")

	cfg, err := config.Load(config.Options{
		Environ: environ,
		EnvFile: map[string]string{"SERVER_PORT": "2222"},
		Flags:   map[string]any{"server.port": 3333},
	})
	require.NoError(t, err)

	assert.Equal(t, 3333, cfg.Server.Port, "a flag must win over both the env file and the environment")
	assert.Equal(t, config.LayerFlag, cfg.Origin("server.port"))
}

func TestFlagBeatsSystemEnvironmentWithoutEnvFile(t *testing.T) {
	environ := append(baseEnv(), "SERVER_PORT=1111")

	cfg, err := config.Load(config.Options{
		Environ: environ,
		Flags:   map[string]any{"server.port": 3333},
	})
	require.NoError(t, err)

	assert.Equal(t, 3333, cfg.Server.Port)
}

func TestConfigFileBeatsDefaultsAndLosesToEnvironment(t *testing.T) {
	path := writeConfig(t, `{
		"server": {"port": 4444, "host": "from-file"},
		"log": {"level": "warn"}
	}`)

	// The file wins over the default, and the environment wins over the file.
	environ := append(baseEnv(), "SERVER_PORT=5555")
	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, 5555, cfg.Server.Port, "the environment must win over the config file")
	assert.Equal(t, "from-file", cfg.Server.Host, "a key no other source sets keeps the file value")
	assert.Equal(t, config.LogWarn, cfg.Log.Level, "the file must win over the default")
	assert.Equal(t, config.LayerConfigFile, cfg.Origin("log.level"))
	assert.Equal(t, config.LayerSystemEnv, cfg.Origin("server.port"))
}

func TestEnvFileBeatsConfigFile(t *testing.T) {
	path := writeConfig(t, `{"server": {"port": 4444}}`)

	cfg, err := config.Load(config.Options{
		ConfigFile: path,
		Environ:    baseEnv(),
		EnvFile:    map[string]string{"SERVER_PORT": "2222"},
	})
	require.NoError(t, err)

	assert.Equal(t, 2222, cfg.Server.Port)
	assert.Equal(t, config.LayerEnvFile, cfg.Origin("server.port"))
}

func TestNestedLeafOverrideKeepsSiblings(t *testing.T) {
	// The environment sets one leaf inside a section the file also sets; the
	// file's other leaves must survive.
	path := writeConfig(t, `{"database": {"max_conns": 20, "min_conns": 5}}`)
	environ := append(baseEnv(), "DATABASE_MAX_CONNS=30")

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, int32(30), cfg.Database.MaxConns, "the environment wins for the leaf it sets")
	assert.Equal(t, int32(5), cfg.Database.MinConns, "a sibling keeps the file value")
}

func TestOriginReportsTheWinningLayer(t *testing.T) {
	path := writeConfig(t, `{"server": {"port": 4444}}`)

	cfg, err := config.Load(config.Options{
		ConfigFile: path,
		Environ:    append(baseEnv(), "SERVER_PORT=5555"),
		Flags:      map[string]any{"server.port": 6666},
	})
	require.NoError(t, err)

	assert.Equal(t, config.LayerFlag, cfg.Origin("server.port"))
	assert.Equal(t, config.LayerSystemEnv, cfg.Origin("database.url"))
	assert.Equal(t, config.LayerDefault, cfg.Origin("log.level"))
	assert.Empty(t, cfg.Origin("nope"))

	origins := cfg.Origins()
	assert.Equal(t, config.LayerFlag, origins["server.port"])

	// Origins must return a copy: mutating it cannot change the Config.
	origins["server.port"] = "tampered"
	assert.Equal(t, config.LayerFlag, cfg.Origin("server.port"))
}
