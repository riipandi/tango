//go:build debug

package launcher

import (
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseOnlyDebug(t *testing.T, args ...string) string {
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

// TestDBCommandGrammarDebug locks in the debug command set:
// manage, migration, and development-only operations all parse.
func TestDBCommandGrammarDebug(t *testing.T) {
	require.Equal(t, "db dump <mode>", parseOnlyDebug(t, "db", "dump", "all"))
	require.Equal(t, "db dump <mode>", parseOnlyDebug(t, "db", "dump", "data"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "all", "storage/backup/x.dump"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "data", "--force", "x.dump"))
	require.Equal(t, "db restore <mode> <file>", parseOnlyDebug(t, "db", "restore", "schema", "--dry-run", "x.dump"))
	require.Equal(t, "db export <mode>", parseOnlyDebug(t, "db", "export", "all"))
	require.Equal(t, "db import <file>", parseOnlyDebug(t, "db", "import", "storage/backup/x.sql"))
	require.Equal(t, "db migrate:up", parseOnlyDebug(t, "db", "migrate:up"))
	require.Equal(t, "db migrate:down", parseOnlyDebug(t, "db", "migrate:down"))
	require.Equal(t, "db migrate:status", parseOnlyDebug(t, "db", "migrate:status"))
	require.Equal(t, "db migrate:version", parseOnlyDebug(t, "db", "migrate:version"))
	require.Equal(t, "db migrate:create <name>", parseOnlyDebug(t, "db", "migrate:create", "add_users_table"))
	require.Equal(t, "db migrate:fix", parseOnlyDebug(t, "db", "migrate:fix"))
	require.Equal(t, "db migrate:validate", parseOnlyDebug(t, "db", "migrate:validate"))
	require.Equal(t, "db migrate:reset", parseOnlyDebug(t, "db", "migrate:reset", "--force"))
	require.Equal(t, "db migrate:reset", parseOnlyDebug(t, "db", "migrate:reset", "--up"))
}

// TestMigrateResetUp rolls everything back and re-applies it in one
// command, ending at the highest version with no pending files.
func TestMigrateResetUp(t *testing.T) {
	chdirRepoRoot(t)

	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)

	out, err := runMigrate(t, true, strings.NewReader("\n"), "db", "migrate:reset", "--up", "--force")
	require.NoError(t, err)
	assert.Contains(t, out, "rolled back")
	assert.Contains(t, out, "applied")
	assert.Contains(t, out, "00031")

	out, err = runMigrate(t, false, nil, "db", "migrate:version")
	require.NoError(t, err)
	assert.Contains(t, out, "current: 31")
}
