//go:build debug

package launcher

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

// parseOnlyDebug resolves the selected command path without
// running it, with the debug-only plugins (secrets, db) registered.
func parseOnlyDebug(t *testing.T, args ...string) string {
	t.Helper()

	base := []kong.Option{
		kong.Name("tango"),
		kong.UsageOnError(),
		versionVars(),
	}
	base = append(base, secretsOptions()...)
	base = append(base, dbOptions()...)

	parser, err := kong.New(&CLI{}, base...)
	require.NoError(t, err)

	kctx, err := parser.Parse(args)
	require.NoError(t, err, "args: %v", args)
	return kctx.Command()
}

// TestMigrateCommandGrammarDebug locks in the debug command set:
// all subcommands parse, including the development-only ones.
func TestMigrateCommandGrammarDebug(t *testing.T) {
	require.Equal(t, "migrate up", parseOnlyDebug(t, "migrate", "up"))
	require.Equal(t, "migrate down", parseOnlyDebug(t, "migrate", "down"))
	require.Equal(t, "migrate status", parseOnlyDebug(t, "migrate", "status"))
	require.Equal(t, "migrate version", parseOnlyDebug(t, "migrate", "version"))
	require.Equal(t, "migrate create <name>", parseOnlyDebug(t, "migrate", "create", "add_users_table"))
	require.Equal(t, "migrate fix", parseOnlyDebug(t, "migrate", "fix"))
	require.Equal(t, "migrate validate", parseOnlyDebug(t, "migrate", "validate"))
	require.Equal(t, "migrate reset", parseOnlyDebug(t, "migrate", "reset", "--force"))
}
