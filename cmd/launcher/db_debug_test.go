//go:build debug

package launcher

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestDBCommandGrammar locks in the db command group: the four
// subcommands parse with their modes and file arguments.
func TestDBCommandGrammar(t *testing.T) {
	require.Equal(t, "db dump <mode>", parseOnlyDebug(t, "db", "dump", "all"))
	require.Equal(t, "db dump <mode>", parseOnlyDebug(t, "db", "dump", "data"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "all", "storage/backup/x.dump"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "data", "--force", "x.dump"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "schema", "--dry-run", "x.dump"))
	require.Equal(t, "db export <mode>", parseOnlyDebug(t, "db", "export", "all"))
	require.Equal(t, "db import <file>", parseOnlyDebug(t, "db", "import", "storage/backup/x.sql"))
}
