//go:build !debug

package launcher

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/require"
)

// TestMigrateCommandGrammar locks in the release command set:
// up, down, and status exist; the development-only commands do not.
func TestMigrateCommandGrammar(t *testing.T) {
	require.Equal(t, "migrate up", parseOnly(t, "migrate", "up"))
	require.Equal(t, "migrate down", parseOnly(t, "migrate", "down"))
	require.Equal(t, "migrate status", parseOnly(t, "migrate", "status"))
	require.Equal(t, "migrate version", parseOnly(t, "migrate", "version"))
	require.Equal(t, "migrate db dump <mode>", parseOnly(t, "migrate", "db", "dump", "all"))

	parser, err := kong.New(&CLI{}, kong.Name("tango"))
	require.NoError(t, err)

	_, err = parser.Parse([]string{"migrate", "create", "x"})
	require.Error(t, err, "create must not exist in release builds")
	_, err = parser.Parse([]string{"migrate", "reset"})
	require.Error(t, err, "reset must not exist in release builds")
}
