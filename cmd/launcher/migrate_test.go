//go:build !debug

package launcher

import (
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parseOnly resolves the selected command path without running it.
func parseOnly(t *testing.T, args ...string) string {
	t.Helper()

	parser, err := kong.New(&CLI{},
		kong.Name("tango"),
		kong.UsageOnError(),
		versionVars(),
	)
	require.NoError(t, err)

	kctx, err := parser.Parse(args)
	require.NoError(t, err, "args: %v", args)
	return kctx.Command()
}

// runMigrate runs a migrate command and returns its captured
// stdout plus any execution error, with the destructive-action
// gates switched to the given stdin source.
func runMigrate(t *testing.T, interactive bool, stdin io.Reader, args ...string) (string, error) {
	t.Helper()

	prevReader, prevInteractive := stdinReader, stdinIsInteractive
	stdinReader = stdin
	stdinIsInteractive = func() bool { return interactive }
	t.Cleanup(func() { stdinReader, stdinIsInteractive = prevReader, prevInteractive })

	parser, err := kong.New(&CLI{},
		kong.Name("tango"),
		kong.Writers(io.Discard, io.Discard),
		versionVars(),
	)
	require.NoError(t, err)
	kctx, err := parser.Parse(args)
	require.NoError(t, err, "args: %v", args)

	out := captureStdout(t, func() {
		err = kctx.Run(&CLI{})
	})
	return out, err
}

// TestMigrateCommandGrammar locks in the release command set:
// up, down, and status exist; the development-only commands do not.
func TestMigrateCommandGrammar(t *testing.T) {
	require.Equal(t, "migrate up", parseOnly(t, "migrate", "up"))
	require.Equal(t, "migrate down", parseOnly(t, "migrate", "down"))
	require.Equal(t, "migrate status", parseOnly(t, "migrate", "status"))

	parser, err := kong.New(&CLI{}, kong.Name("tango"))
	require.NoError(t, err)

	_, err = parser.Parse([]string{"migrate", "create", "x"})
	require.Error(t, err, "create must not exist in release builds")
	_, err = parser.Parse([]string{"migrate", "reset"})
	require.Error(t, err, "reset must not exist in release builds")
}

// TestMigrateLifecycle runs up, a guarded down, and status against
// the shared testcontainer database, configured through the
// environment the same way operators do it.
func TestMigrateLifecycle(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)

	out, err := runMigrate(t, true, strings.NewReader("\n"), "migrate", "up")
	require.NoError(t, err)
	assert.Contains(t, out, "00001")

	// Dry-run: reports the target without rolling it back.
	out, err = runMigrate(t, true, strings.NewReader("\n"), "migrate", "down", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "would roll back")
	assert.Contains(t, out, "00001")

	// Non-interactive stdin refuses even an explicit Enter.
	_, err = runMigrate(t, false, strings.NewReader("\n"), "migrate", "down")
	assert.ErrorContains(t, err, "--force")

	// Interactive decline aborts.
	_, err = runMigrate(t, true, strings.NewReader("\n"), "migrate", "down")
	assert.ErrorContains(t, err, "aborted")

	// Interactive yes proceeds, as does --force on any stdin.
	out, err = runMigrate(t, true, strings.NewReader("y\n"), "migrate", "down")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back")

	// Re-apply, then roll back again via --force: no prompt, no
	// stdin needed.
	_, err = runMigrate(t, false, nil, "migrate", "up")
	require.NoError(t, err)
	out, err = runMigrate(t, false, nil, "migrate", "down", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back")

	out, err = runMigrate(t, false, nil, "migrate", "status")
	require.NoError(t, err)
	assert.Contains(t, out, "pending")
}
