package launcher

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
		kong.Name(config.AppName),
		kong.Description("A fullstack web application built with Go, Chi, and React."),
		kong.Writers(io.Discard, io.Discard),
		versionVars(),
	}
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
			kong.Name(config.AppName),
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
		kong.Name(config.AppName),
		kong.Writers(help, io.Discard),
		kong.Exit(func(int) {}), // --help must not os.Exit in tests
		versionVars(),
	}
	parser, err := kong.New(&CLI{}, opts...)
	require.NoError(t, err)

	// --help prints the help, then (with the stubbed exit) parse
	// falls through to a missing-command error. The printed help is
	// what matters here.
	_, _ = parser.Parse([]string{"--help"})

	assert.Contains(t, help.String(), "serve")
	assert.Contains(t, help.String(), "db")
	assert.Contains(t, help.String(), "secrets")
	assert.Contains(t, help.String(), "health")
}

func TestFlagOverrides(t *testing.T) {
	empty, err := flagOverrides("", "")
	require.NoError(t, err)
	assert.Empty(t, empty)

	hostOnly, err := flagOverrides("127.0.0.1", "")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"host": "127.0.0.1"}, hostOnly)

	portOnly, err := flagOverrides("", ":9999")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"port": 9999}, portOnly)

	both, err := flagOverrides("h", "1")
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"host": "h", "port": 1}, both)

	_, err = flagOverrides("", "bogus")
	assert.ErrorContains(t, err, "invalid --port")
}

func TestRunCLIParseError(t *testing.T) {
	err := RunCLI([]string{"--nope"}, kong.Writers(io.Discard, io.Discard))
	require.Error(t, err)
}

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "2.00 MB", formatSize(2*1024*1024))
	assert.Equal(t, "1.50 MB", formatSize(1536*1024))
	assert.Equal(t, "512.0 KB", formatSize(512*1024))
	assert.Equal(t, "0.5 KB", formatSize(512))
}

func TestHealthStaticPrintsBinaryInfo(t *testing.T) {
	out := runCommand(t, "hc")

	assert.Contains(t, out, "status:    healthy")
	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Contains(t, out, exe)
}

func TestHealthLiveOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// runCommand already captures command output.
	out := runCommand(t, "hc", "--live", "--addr", upstream.URL)
	assert.Contains(t, out, "ok")
}

// chdirRepoRoot moves the test working directory to the repository
// root — debug builds resolve the disk migration source relative
// to it — and restores it when the test ends.
func chdirRepoRoot(t *testing.T) {
	t.Helper()

	orig, err := os.Getwd()
	require.NoError(t, err)

	dir := orig
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "Taskfile.yml")); statErr == nil {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repository root not found")
		}
		dir = parent
	}

	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

// captureStdout redirects os.Stdout so fmt.Print* output is captured.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	out, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(out)
}
