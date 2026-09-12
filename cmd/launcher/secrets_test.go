package launcher

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretsRegistered(t *testing.T) {
	help := &strings.Builder{}
	parser, err := kong.New(&CLI{},
		kong.Name(config.AppName),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}), // --help must not os.Exit in tests
		versionVars(),
	)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"--help"})
	assert.Contains(t, help.String(), "secrets")
}

func TestSecretsGeneratesKeys(t *testing.T) {
	t.Chdir(t.TempDir())

	out := captureStdout(t, func() {
		require.NoError(t, RunCLI([]string{"secrets"}, kong.Writers(io.Discard, io.Discard)))
	})

	assert.Contains(t, out, "APP_SECRET_KEY=")
	assert.Contains(t, out, "JWT_PRIVATE_KEY=")

	// PEM key pairs land in <data-dir>/keys (default storage/keys).
	for _, name := range []string{"private_key.pem", "public_key.pem"} {
		data, err := os.ReadFile("storage/keys/" + name)
		require.NoError(t, err)
		assert.Contains(t, string(data), "KEY-----")
	}

	// --data-dir relocates the keys: the default tree stays empty.
	custom := t.TempDir()
	out = captureStdout(t, func() {
		require.NoError(t, RunCLI([]string{"--data-dir", custom, "secrets"}, kong.Writers(io.Discard, io.Discard)))
	})
	assert.Contains(t, out, "APP_SECRET_KEY=")
	for _, name := range []string{"private_key.pem", "public_key.pem"} {
		data, err := os.ReadFile(custom + "/keys/" + name)
		require.NoError(t, err)
		assert.Contains(t, string(data), "KEY-----")
	}
}

func TestSecretsApplyWritesEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := dir + "/.env.local"
	require.NoError(t, os.WriteFile(envFile, []byte("PORT=3080\n"), 0o600))

	captureStdout(t, func() {
		require.NoError(t, RunCLI([]string{"--data-dir", dir, "secrets", "--apply", "--out", envFile}, kong.Writers(io.Discard, io.Discard)))
	})

	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	for _, key := range []string{"APP_SECRET_KEY=", "JWT_PRIVATE_KEY=", "JWT_PUBLIC_KEY=", "JWT_SECRET_KEY="} {
		assert.Contains(t, string(data), key)
	}
}

func TestUpsertEnvFile(t *testing.T) {
	envFile := t.TempDir() + "/.env"
	require.NoError(t, os.WriteFile(envFile, []byte("A=1\nB=2\n"), 0o600))

	require.NoError(t, upsertEnvFile(envFile, "B", "20"))
	require.NoError(t, upsertEnvFile(envFile, "C", "30"))

	data, err := os.ReadFile(envFile)
	require.NoError(t, err)
	assert.Equal(t, "A=1\nB=20\nC=30\n", string(data))
}
