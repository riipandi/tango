package launcher

import (
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

	cli := newCLI()
	parser, err := kong.New(cli,
		kong.Name("tango"),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.Writers(io.Discard, io.Discard),
	)
	require.NoError(t, err)

	ctx, err := parser.Parse(args)
	require.NoError(t, err, "args: %v", args)

	out := captureStdout(t, func() {
		require.NoError(t, ctx.Run(cli))
	})
	return out
}

func TestVersionDefault(t *testing.T) {
	out := runCommand(t, "version")

	want := config.AppName + " " + config.AppVersion + " " + config.Platform
	assert.Contains(t, out, want)
	assert.Contains(t, out, "("+config.BuildHash)
}

func TestVersionShort(t *testing.T) {
	out := runCommand(t, "version", "--short")

	assert.Equal(t, config.AppVersion+" ("+config.BuildHash+")\n", out)
}

func TestVersionSemantic(t *testing.T) {
	out := runCommand(t, "version", "--semantic")

	assert.Equal(t, config.AppVersion+"\n", out)
}

func TestHelpListsCommands(t *testing.T) {
	cli := newCLI()
	help := &strings.Builder{}
	parser, err := kong.New(cli,
		kong.Name("tango"),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}), // --help must not os.Exit in tests
	)
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
	err := runCLI([]string{"--nope"}, kong.Writers(io.Discard, io.Discard))
	require.Error(t, err)
}

func TestRunCLIVersion(t *testing.T) {
	out := captureStdout(t, func() {
		require.NoError(t, runCLI([]string{"version"}, kong.Writers(io.Discard, io.Discard)))
	})
	assert.Contains(t, out, config.AppVersion)
}
