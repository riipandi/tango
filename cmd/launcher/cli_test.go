package launcher

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// runCommand parses and runs the CLI, returning combined command
// output (fmt.Print* captured from stdout).
func runCommand(t *testing.T, args ...string) string {
	t.Helper()

	cli := &CLI{}
	base := []kong.Option{
		kong.Name("tango"),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.Writers(io.Discard, io.Discard),
		versionVars(),
	}
	base = append(base, secretsOptions()...)
	parser, err := kong.New(cli, base...)
	require.NoError(t, err)

	ctx, err := parser.Parse(args)
	require.NoError(t, err, "args: %v", args)

	out := captureStdout(t, func() {
		require.NoError(t, ctx.Run(cli))
	})
	return out
}

func TestVersionFlag(t *testing.T) {
	want := fmt.Sprintf("%s %s %s (%s %s)",
		config.AppName, config.AppVersion, config.Platform, config.BuildHash, config.BuildDate)

	for _, args := range [][]string{{"--version"}, {"-V"}} {
		out := &strings.Builder{}
		parser, err := kong.New(&CLI{},
			kong.Name("tango"),
			kong.Writers(out, io.Discard),
			kong.Exit(func(int) {}), // --version exits; stub it for tests
			versionVars(),
		)
		require.NoError(t, err)

		_, _ = parser.Parse(args)
		assert.Contains(t, out.String(), want, "args: %v", args)
	}
}

func TestHelpListsCommands(t *testing.T) {
	help := &strings.Builder{}
	opts := []kong.Option{
		kong.Name("tango"),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}), // --help must not os.Exit in tests
		versionVars(),
	}
	opts = append(opts, secretsOptions()...)
	parser, err := kong.New(&CLI{}, opts...)
	require.NoError(t, err)

	// --help prints the help, then (with the stubbed exit) parse
	// falls through to a missing-command error. The printed help is
	// what matters here.
	_, _ = parser.Parse([]string{"--help"})

	assert.Contains(t, help.String(), "serve")
	assert.Contains(t, help.String(), "migrate")
	assert.Contains(t, help.String(), "health")
}

func TestFlagOverrides(t *testing.T) {
	assert.Empty(t, flagOverrides("", ""))
	assert.Equal(t, map[string]any{"host": "127.0.0.1"}, flagOverrides("127.0.0.1", ""))
	assert.Equal(t, map[string]any{"port": 9999}, flagOverrides("", ":9999"))
	assert.Equal(t, map[string]any{"host": "h", "port": 1}, flagOverrides("h", "1"))
}

func TestRunCLIParseError(t *testing.T) {
	err := RunCLI([]string{"--nope"}, kong.Writers(io.Discard, io.Discard))
	require.Error(t, err)
}
