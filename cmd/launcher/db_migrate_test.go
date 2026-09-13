package launcher

import (
	"io"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrateLifecycle runs up, a guarded down, and status against
// the shared testcontainer database, configured through the
// environment the same way operators do it. It runs in both build
// variants — the command set only differs in development-only
// subcommands, not in these.
func TestMigrateLifecycle(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)

	// Deterministic starting state: other lifecycle tests share
	// this container, so roll back anything they applied first.
	_, err := database.MigrateDownTo(t.Context(), pg.DSN, 0)
	require.NoError(t, err)

	out, err := runMigrate(t, true, strings.NewReader("\n"), "db", "migrate:up")
	require.NoError(t, err)
	assert.Contains(t, out, "00025")

	// Partial apply: up to an already-applied version is a no-op.
	out, err = runMigrate(t, true, strings.NewReader("\n"), "db", "migrate:up", "--to", "25")
	require.NoError(t, err)
	assert.Contains(t, out, "nothing to migrate")

	out, err = runMigrate(t, false, nil, "db", "migrate:version")
	require.NoError(t, err)
	assert.Contains(t, out, "current: 25")

	// Dry-run: reports the target without rolling it back.
	out, err = runMigrate(t, true, strings.NewReader("\n"), "db", "migrate:down", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, out, "would roll back")
	assert.Contains(t, out, "00025")

	// Non-interactive stdin refuses even an explicit Enter.
	_, err = runMigrate(t, false, strings.NewReader("\n"), "db", "migrate:down")
	assert.ErrorContains(t, err, "--force")

	// Interactive decline aborts.
	_, err = runMigrate(t, true, strings.NewReader("\n"), "db", "migrate:down")
	assert.ErrorContains(t, err, "aborted")

	// Interactive yes proceeds, as does --force on any stdin.
	out, err = runMigrate(t, true, strings.NewReader("y\n"), "db", "migrate:down")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back")

	// Re-apply, then roll back again via --force: no prompt, no
	// stdin needed.
	_, err = runMigrate(t, false, nil, "db", "migrate:up")
	require.NoError(t, err)
	out, err = runMigrate(t, false, nil, "db", "migrate:down", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back")

	out, err = runMigrate(t, false, nil, "db", "migrate:status")
	require.NoError(t, err)
	assert.Contains(t, out, "pending")
}

// parseOnly resolves the selected command path without running it.
// Debug-only plugins are appended by the per-variant test files.
func parseOnly(t *testing.T, args ...string) string {
	t.Helper()

	parser, err := kong.New(&CLI{},
		kong.Name(config.AppName),
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
		kong.Name(config.AppName),
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
