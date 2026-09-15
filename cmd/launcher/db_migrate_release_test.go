//go:build !debug

package launcher

import (
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/require"
)

// TestDBCommandGrammar locks in the release command set.
func TestDBCommandGrammar(t *testing.T) {
	require.Equal(t, "db dump <mode>", parseOnly(t, "db", "dump", "all"))
	require.Equal(t, "db dump <mode>", parseOnly(t, "db", "dump", "data"))
	require.Equal(t, "db restore <mode> <file>", parseOnly(t, "db", "restore", "all", "storage/backup/x.dump"))
	require.Equal(t, "db export <mode>", parseOnly(t, "db", "export", "all"))
	require.Equal(t, "db import <file>", parseOnly(t, "db", "import", "storage/backup/x.sql"))
	require.Equal(t, "db migrate:up", parseOnly(t, "db", "migrate:up"))
	require.Equal(t, "db migrate:down", parseOnly(t, "db", "migrate:down"))
	require.Equal(t, "db migrate:status", parseOnly(t, "db", "migrate:status"))
	require.Equal(t, "db migrate:version", parseOnly(t, "db", "migrate:version"))

	parser, err := kong.New(&CLI{}, kong.Name(config.AppName))
	require.NoError(t, err)

	_, err = parser.Parse([]string{"db", "migrate:create", "x"})
	require.Error(t, err, "migrate:create must not exist in release builds")
	_, err = parser.Parse([]string{"db", "migrate:reset"})
	require.Error(t, err, "migrate:reset must not exist in release builds")
}
