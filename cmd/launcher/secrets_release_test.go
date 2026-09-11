//go:build !debug

package launcher

import (
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The secrets command is a debug-build-only tool: in release builds
// it is not registered at all.
func TestSecretsUnavailableInRelease(t *testing.T) {
	opts := []kong.Option{
		kong.Name("tango"),
		kong.Writers(io.Discard, io.Discard),
		kong.Exit(func(int) {}),
		versionVars(),
	}
	opts = append(opts, secretsOptions()...)
	parser, err := kong.New(&CLI{}, opts...)
	require.NoError(t, err)

	_, err = parser.Parse([]string{"secrets"})
	require.Error(t, err, "secrets must not be registered in release builds")

	help := &strings.Builder{}
	opts2 := []kong.Option{
		kong.Name("tango"),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}),
	}
	opts2 = append(opts2, secretsOptions()...)
	parser2, err := kong.New(&CLI{}, opts2...)
	require.NoError(t, err)
	_, _ = parser2.Parse([]string{"--help"})
	assert.NotContains(t, help.String(), "secrets")
}
