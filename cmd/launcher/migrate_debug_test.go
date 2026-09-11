//go:build debug

package launcher

import (
	"testing"

	"github.com/alecthomas/kong"
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

// TestMigrateCommandGrammarDebug locks in the debug command set:
// all five subcommands parse, including the development-only ones.
func TestMigrateCommandGrammarDebug(t *testing.T) {
	require.Equal(t, "migrate up", parseOnly(t, "migrate", "up"))
	require.Equal(t, "migrate down", parseOnly(t, "migrate", "down"))
	require.Equal(t, "migrate status", parseOnly(t, "migrate", "status"))
	require.Equal(t, "migrate create <name>", parseOnly(t, "migrate", "create", "add_users_table"))
	require.Equal(t, "migrate reset", parseOnly(t, "migrate", "reset", "--force"))
}
