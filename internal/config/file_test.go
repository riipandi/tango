package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
)

func TestConfigFileResolvedFromEnvironment(t *testing.T) {
	path := writeConfig(t, `{"server": {"port": 4444}}`)
	environ := append(baseEnv(), config.FileEnv+"="+path)

	cfg, err := config.Load(config.Options{Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, 4444, cfg.Server.Port)
}

func TestConfigFileFlagBeatsEnvironment(t *testing.T) {
	fromFlag := writeConfig(t, `{"server": {"port": 1111}}`)
	fromEnv := writeConfig(t, `{"server": {"port": 2222}}`)

	cfg, err := config.Load(config.Options{
		ConfigFile: fromFlag,
		Environ:    append(baseEnv(), config.FileEnv+"="+fromEnv),
	})
	require.NoError(t, err)

	assert.Equal(t, 1111, cfg.Server.Port)
}

func TestDefaultConfigFileInWorkingDirectory(t *testing.T) {
	// With no source naming a file, app.config.json in the working directory is
	// read.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, config.DefaultConfigFile),
		[]byte(`{"server": {"port": 7777}}`),
		0o600,
	))
	t.Chdir(dir)

	cfg, err := config.Load(config.Options{Environ: baseEnv()})
	require.NoError(t, err)

	assert.Equal(t, 7777, cfg.Server.Port)
	assert.Equal(t, config.LayerConfigFile, cfg.Origin("server.port"))
}

func TestMissingDefaultConfigFileIsNotAnError(t *testing.T) {
	// A fresh checkout has no config file and must still run.
	t.Chdir(t.TempDir())

	cfg, err := config.Load(config.Options{Environ: baseEnv()})
	require.NoError(t, err)

	assert.Equal(t, config.Default().Server.Port, cfg.Server.Port)
	assert.Equal(t, config.LayerDefault, cfg.Origin("server.port"))
}

func TestNamedConfigFileMustExist(t *testing.T) {
	_, err := config.Load(config.Options{
		ConfigFile: filepath.Join(t.TempDir(), "absent.json"),
		Environ:    baseEnv(),
	})
	require.ErrorIs(t, err, config.ErrNoConfigFile)
}

func TestConfigFileRejectsMalformedJSON(t *testing.T) {
	path := writeConfig(t, `{"server": {"port":`)

	_, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
	require.Error(t, err)
	assert.NotErrorIs(t, err, config.ErrNoConfigFile)
}

func TestConfigFileIgnoresUnknownKeys(t *testing.T) {
	path := writeConfig(t, `{"server": {"port": 4444, "nope": true}, "unknown": {"a": 1}}`)

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
	require.NoError(t, err)

	assert.Equal(t, 4444, cfg.Server.Port)
	assert.Empty(t, cfg.Origin("server.nope"))
	assert.Empty(t, cfg.Origin("unknown.a"))
}

func TestInterpolationFromEnvironment(t *testing.T) {
	path := writeConfig(t, `{
		"database": {"url": "env:TEST_DSN"},
		"server": {"base_url": "http://${TEST_HOST}:3080"},
		"log": {"level": "info"}
	}`)

	environ := append(baseEnv(),
		"TEST_DSN="+dsn,
		"TEST_HOST=example.test",
	)

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, dsn, cfg.Database.URL)
	assert.Equal(t, "http://example.test:3080", cfg.Server.BaseURL)
	assert.Equal(t, "info", cfg.Log.Level, "a value with no directive is untouched")
}

func TestInterpolationExpandsEveryDirective(t *testing.T) {
	path := writeConfig(t, `{"storage": {"local_path": "${A}-${B}-${C}"}}`)
	environ := append(baseEnv(), "A=1", "B=2", "C=3")

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: environ})
	require.NoError(t, err)

	assert.Equal(t, "1-2-3", cfg.Storage.LocalPath)
}

func TestInterpolationLeavesBareDollarAlone(t *testing.T) {
	// A password commonly contains $; expanding it would corrupt the value.
	path := writeConfig(t, `{"storage": {"local_path": "/srv/$data$$x"}}`)

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
	require.NoError(t, err)

	assert.Equal(t, "/srv/$data$$x", cfg.Storage.LocalPath)
}

func TestInterpolationLeavesUnterminatedDirectiveAlone(t *testing.T) {
	path := writeConfig(t, `{"storage": {"local_path": "/srv/${5"}}`)

	cfg, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
	require.NoError(t, err)

	assert.Equal(t, "/srv/${5", cfg.Storage.LocalPath)
}

func TestInterpolationFailsOnMissingVariable(t *testing.T) {
	for _, body := range []string{
		`{"database": {"url": "env:NOT_SET_ANYWHERE"}}`,
		`{"database": {"url": "postgres://${NOT_SET_ANYWHERE}/db"}}`,
	} {
		path := writeConfig(t, body)

		_, err := config.Load(config.Options{ConfigFile: path, Environ: baseEnv()})
		require.ErrorIs(t, err, config.ErrUnresolvedVar, body)
	}
}

func TestInterpolationOnlyAppliesToConfigFile(t *testing.T) {
	// The environment is the source interpolation reads; it must not be
	// interpolated itself, or a value containing ${ would be rewritten.
	cfg, err := config.Load(config.Options{
		Environ: append(baseEnv(), "STORAGE_LOCAL_PATH=/srv/${NOPE}"),
	})
	require.NoError(t, err)

	assert.Equal(t, "/srv/${NOPE}", cfg.Storage.LocalPath)
}
