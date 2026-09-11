//go:build debug

package launcher

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSecretsRegisteredInDebug(t *testing.T) {
	help := &strings.Builder{}
	opts := []kong.Option{
		kong.Name("tango"),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}),
	}
	opts = append(opts, secretsOptions()...)
	parser, err := kong.New(&CLI{}, opts...)
	require.NoError(t, err)

	_, _ = parser.Parse([]string{"--help"})
	assert.Contains(t, help.String(), "secrets")
	assert.NotContains(t, help.String(), "--apply", "secrets flags belong to the secrets command, not the root")
}

func TestSecretsGeneratesKeys(t *testing.T) {
	t.Chdir(t.TempDir())

	out := captureStdout(t, func() {
		require.NoError(t, RunCLI([]string{"secrets"}, kong.Writers(io.Discard, io.Discard)))
	})

	assert.Contains(t, out, "APP_SECRET_KEY=")
	assert.Contains(t, out, "JWT_PRIVATE_KEY=")

	// PEM key pairs land in storage/keys.
	for _, name := range []string{"private_key.pem", "public_key.pem"} {
		data, err := os.ReadFile("storage/keys/" + name)
		require.NoError(t, err)
		assert.Contains(t, string(data), "KEY-----")
	}
}
