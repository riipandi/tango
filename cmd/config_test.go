package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
)

// testRoot builds the root command a test runs a subcommand through.
//
// It mirrors the real root command: the config flags are registered, and
// initConfig runs as Before, so a test exercises the same precedence chain the
// binary does. Only the flag names differ from cli_debug.go, which registers
// exactly these two.
func testRoot(out *bytes.Buffer, stdin string, commands ...*cli.Command) *cli.Command {
	return &cli.Command{
		Name:     "tango",
		Writer:   out,
		Reader:   strings.NewReader(stdin),
		Commands: commands,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: config.FlagEnvFile, Usage: "Load environment variables from a file"},
			&cli.StringFlag{Name: config.FlagConfigFile, Usage: "Load configuration from a JSON file"},
		},
		// cli.Exit calls os.Exit, so the error is trapped instead.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Before:         initConfig,
	}
}

// useConfig points the config layer at a file body for this test.
//
// The file is the source of truth, so every command test needs one. It is named
// through CONFIG_FILE rather than --config-file so a test does not have to know
// where the flag belongs in the argument list.
func useConfig(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.config.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	t.Setenv(config.FileEnv, path)
	return path
}

// configFor writes the config file a command test resolves against. The DSN is
// left to the environment, so a test points at its own database by setting
// DATABASE_URL; dataDir, when not empty, is written into the file because the
// data directory has no flag and no variable of its own.
//
// Only database.url is declared, because no command test validates the whole
// configuration: a command that needs one key must not be blocked by a key it
// never reads, and this is the file that proves it.
func configFor(t *testing.T, dataDir string) {
	t.Helper()

	body := `{"database": {"url": "env:DATABASE_URL"}`
	if dataDir != "" {
		body += `, "app": {"data_dir": ` + strconv.Quote(dataDir) + `}`
	}
	useConfig(t, body+"}")
}

// testSecret is a 64-character hex string, the shape of AUTH_SECRET_KEY.
const testSecret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// # config:generate and config:validate

// runConfigGenerate executes config:generate with args and returns stdout.
func runConfigGenerateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := testRoot(&out, "", configGenerateCmd)
	err := root.Run(context.Background(), append([]string{"tango", configGenerateCmd.Name}, args...))
	return out.String(), err
}

// runConfigValidate executes config:validate with args and returns stdout.
func runConfigValidateCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := testRoot(&out, "", configValidateCmd)
	err := root.Run(context.Background(), append([]string{"tango", configValidateCmd.Name}, args...))
	return out.String(), err
}

func TestConfigGenerateWritesEveryKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.config.json")

	out, err := runConfigGenerateCmd(t, "--output="+path)
	require.NoError(t, err)
	assert.Contains(t, out, "created")
	assert.Contains(t, out, "status: config file ready")

	written, err := os.ReadFile(path)
	require.NoError(t, err)

	// The file must load back as a valid configuration, which is the only
	// statement that matters about a generated file.
	cfg, err := config.Load(config.Options{
		ConfigFile: path,
		Environ: []string{
			"DATABASE_URL=postgresql://user:pass@localhost:5432/tango?sslmode=disable",
			"AUTH_SECRET_KEY=" + testSecret,
			"APP_SECRET_KEY=" + testSecret,
			"AUTH_PRIVATE_KEY=" + testSecret,
			"AUTH_PUBLIC_KEY=" + testSecret,
		},
	})
	require.NoError(t, err)
	require.NoError(t, cfg.Validate(), string(written))
}

func TestConfigGenerateNeverWritesALiteralSecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.config.json")
	_, err := runConfigGenerateCmd(t, "--output="+path)
	require.NoError(t, err)

	written, err := os.ReadFile(path)
	require.NoError(t, err)

	// Each secret must be a directive. A literal here would be committed by
	// accident, because the file is meant to be readable.
	for _, key := range []string{"app.secret_key", "auth.private_key", "auth.public_key", "auth.secret_key", "database.url"} {
		assert.Contains(t, string(written), `"`+lastSegment(key)+`": "env:`+config.EnvName(key)+`"`)
	}
}

// lastSegment returns the field name of a config key.
func lastSegment(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '.' {
			return key[i+1:]
		}
	}
	return key
}

func TestConfigGenerateRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.config.json")
	require.NoError(t, os.WriteFile(path, []byte("keep me"), 0o600))

	_, err := runConfigGenerateCmd(t, "--output="+path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--overwrite")

	// The existing file must be untouched: losing a hand-edited config to a
	// mistyped command is the failure this guard exists for.
	kept, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "keep me", string(kept))
}

func TestConfigGenerateOverwriteReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.config.json")
	require.NoError(t, os.WriteFile(path, []byte("replace me"), 0o600))

	_, err := runConfigGenerateCmd(t, "--output="+path, "--overwrite")
	require.NoError(t, err)

	written, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(written), `"port": 3080`)
	assert.NotContains(t, string(written), "replace me")
}

func TestConfigGenerateDefaultsToTheConfigFile(t *testing.T) {
	// With no --output the file lands where the loader looks for it, which is
	// what makes the command the first step of a fresh checkout.
	dir := t.TempDir()
	t.Setenv(config.FileEnv, filepath.Join(dir, config.DefaultConfigFile))

	out, err := runConfigGenerateCmd(t)
	require.NoError(t, err)
	assert.Contains(t, out, config.DefaultConfigFile)

	_, statErr := os.Stat(filepath.Join(dir, config.DefaultConfigFile))
	require.NoError(t, statErr)
}

func TestConfigValidateAcceptsAGoodFile(t *testing.T) {
	useConfig(t, `{
	  "database": {"url": "env:DATABASE_URL"},
	  "auth": {"secret_key": "env:AUTH_SECRET_KEY"}
	}`)
	t.Setenv("DATABASE_URL", "postgresql://user:pass@localhost:5432/tango?sslmode=disable")
	t.Setenv("AUTH_SECRET_KEY", testSecret)

	out, err := runConfigValidateCmd(t)
	require.NoError(t, err)
	assert.Contains(t, out, "valid")
	assert.Contains(t, out, "status:")
	assert.Contains(t, out, "localhost:5432/tango")
}

func TestConfigValidateReportsEveryProblem(t *testing.T) {
	// Two mistakes at once: both must be named, so one run fixes the file.
	useConfig(t, `{
	  "database": {"url": "env:DATABASE_URL"},
	  "server": {"port": 0},
	  "log": {"level": "loud"}
	}`)
	t.Setenv("DATABASE_URL", "postgresql://user:pass@localhost:5432/tango?sslmode=disable")

	_, err := runConfigValidateCmd(t)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "server.port")
	assert.Contains(t, err.Error(), "log.level")
	assert.Contains(t, err.Error(), "auth")
}

func TestConfigValidateReportsAnUnresolvedVariable(t *testing.T) {
	useConfig(t, `{"database": {"url": "env:NOT_SET_ANYWHERE"}}`)

	_, err := runConfigValidateCmd(t)
	require.ErrorIs(t, err, config.ErrInvalid)
	assert.Contains(t, err.Error(), "NOT_SET_ANYWHERE")
}

func TestConfigValidateReportsAMissingFile(t *testing.T) {
	t.Setenv(config.FileEnv, filepath.Join(t.TempDir(), "absent.json"))

	_, err := runConfigValidateCmd(t)
	require.ErrorIs(t, err, config.ErrNoConfigFile)
}

func TestConfigValidateUsesTheConfigFileFlag(t *testing.T) {
	// --config-file names the file to check, so a file outside the working
	// directory can be validated before it is deployed.
	path := writeTestConfigFile(t, `{
	  "database": {"url": "env:DATABASE_URL"},
	  "auth": {"secret_key": "env:AUTH_SECRET_KEY"}
	}`)
	t.Setenv("DATABASE_URL", "postgresql://user:pass@localhost:5432/tango?sslmode=disable")
	t.Setenv("AUTH_SECRET_KEY", testSecret)

	out, err := runConfigValidateCmd(t, "--config-file="+path)
	require.NoError(t, err)
	assert.Contains(t, out, path)
}

// writeTestConfigFile writes a config file and returns its path.
func writeTestConfigFile(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app.config.json")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}
