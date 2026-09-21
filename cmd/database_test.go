//go:build debug

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/database"
)

// runDBExportCmd runs db:export with the given stdin and arguments.
func runDBExportCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runDBCmd(t, dbExportCmd, "", args...)
}

// runDBImportCmd runs db:import with the given stdin and arguments.
func runDBImportCmd(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()
	return runDBCmd(t, dbImportCmd, stdin, args...)
}

// runDBCmd runs one database command against a throwaway data directory, so a
// test never writes into the checkout.
func runDBCmd(t *testing.T, command *cli.Command, stdin string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := &cli.Command{
		Name:     "tango",
		Writer:   &out,
		Reader:   strings.NewReader(stdin),
		Commands: []*cli.Command{dbExportCmd, dbImportCmd},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "env-file"},
			&cli.StringFlag{Name: "data-dir", Value: t.TempDir()},
		},
		// cli.Exit calls os.Exit, so the error is trapped instead.
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}

	err := root.Run(t.Context(), append([]string{"tango", command.Name}, args...))
	return out.String(), err
}

// exportTo writes a dump to an explicit path and returns it.
func exportTo(t *testing.T, envFile, path string, extra ...string) {
	t.Helper()
	args := append([]string{"--env-file=" + envFile, "--output=" + path}, extra...)
	_, err := runDBExportCmd(t, args...)
	require.NoError(t, err)
}

// A dump lands in the data directory when --output is not given, and the
// directory is created on the way.
func TestDBExportWritesToTheDataDirectory(t *testing.T) {
	envFile := migratedDatabase(t)
	dataDir := filepath.Join(t.TempDir(), "storage")

	out, err := runDBExportCmd(t, "--env-file="+envFile, "--data-dir="+dataDir)
	require.NoError(t, err)

	// The report names the database it read and the file it wrote, as one
	// aligned block with no blank line between the two.
	assert.Contains(t, out, "database:  ")
	assert.Contains(t, out, "written:   ")
	assert.Contains(t, out, dataDir)
	assert.NotContains(t, out, "\n\n")

	entries, err := os.ReadDir(filepath.Join(dataDir, backupDir))
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.True(t, strings.HasSuffix(entries[0].Name(), ".sql"))
	assert.Regexp(t, `^tango-\d{8}_\d{4}\.sql$`, entries[0].Name())
}

// The generated name is readable to the minute, so two exports inside one minute
// land on the same path. The first dump must not be lost silently.
func TestDBExportRefusesToOverwriteAGeneratedDump(t *testing.T) {
	envFile := migratedDatabase(t)
	dataDir := filepath.Join(t.TempDir(), "storage")

	_, err := runDBExportCmd(t, "--env-file="+envFile, "--data-dir="+dataDir)
	require.NoError(t, err)

	_, err = runDBExportCmd(t, "--env-file="+envFile, "--data-dir="+dataDir)
	require.ErrorIs(t, err, ErrDumpFileExists)

	// --overwrite is the way to ask for the replacement.
	_, err = runDBExportCmd(t, "--env-file="+envFile, "--data-dir="+dataDir, "--overwrite")
	require.NoError(t, err)
}

// --output writes exactly where it is told.
func TestDBExportHonorsOutput(t *testing.T) {
	envFile := migratedDatabase(t)
	path := filepath.Join(t.TempDir(), "dump.sql")

	out, err := runDBExportCmd(t, "--env-file="+envFile, "--output="+path)
	require.NoError(t, err)
	assert.Contains(t, out, path)

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(content), "-- tango database dump")
}

// A missing parent directory must fail rather than be created: the path came
// from the user, so a typo has to be visible.
func TestDBExportRefusesMissingOutputDirectory(t *testing.T) {
	envFile := migratedDatabase(t)

	_, err := runDBExportCmd(t, "--env-file="+envFile,
		"--output="+filepath.Join(t.TempDir(), "missing", "dump.sql"))
	require.Error(t, err)
}

// --schema-only and --data-only describe opposite dumps, so asking for both is a
// mistake rather than a preference.
func TestDBExportRejectsConflictingSelection(t *testing.T) {
	envFile := migratedDatabase(t)

	_, err := runDBExportCmd(t, "--env-file="+envFile, "--schema-only", "--data-only",
		"--output="+filepath.Join(t.TempDir(), "dump.sql"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

// The schema dump carries DDL, the data dump carries rows, and neither carries
// the other.
func TestDBExportSelection(t *testing.T) {
	envFile := migratedDatabase(t)

	schemaPath := filepath.Join(t.TempDir(), "schema.sql")
	exportTo(t, envFile, schemaPath, "--schema-only")
	schemaDump, err := os.ReadFile(schemaPath)
	require.NoError(t, err)
	assert.Contains(t, string(schemaDump), "CREATE TABLE")
	assert.NotContains(t, string(schemaDump), "FROM stdin")

	dataPath := filepath.Join(t.TempDir(), "data.sql")
	exportTo(t, envFile, dataPath, "--data-only")
	dataDump, err := os.ReadFile(dataPath)
	require.NoError(t, err)
	assert.Contains(t, string(dataDump), "FROM stdin")
	assert.NotContains(t, string(dataDump), "CREATE TABLE")
}

// A dump exported from one database loads into another, which is the whole point
// of the pair.
func TestDBImportRoundTrips(t *testing.T) {
	sourceEnv := migratedDatabase(t)
	_, err := runMigrateSeedCmd(t, "", "--env-file="+sourceEnv, "--force")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "dump.sql")
	exportTo(t, sourceEnv, path, "--data-only")

	targetEnv := migratedDatabase(t)
	out, err := runDBImportCmd(t, "", "--env-file="+targetEnv, "--truncate", "--force", path)
	require.NoError(t, err)

	assert.Contains(t, out, "loaded:    ")
	assert.Contains(t, out, path)
	assert.Equal(t, 1, countUsers(t, targetEnv))
}

// Without a file there is nothing to import, so the argument is required.
func TestDBImportRequiresAFile(t *testing.T) {
	envFile := migratedDatabase(t)

	_, err := runDBImportCmd(t, "", "--env-file="+envFile)
	require.Error(t, err)
}

// A file that is not a dump must be reported as such rather than loaded as
// nothing.
func TestDBImportRejectsForeignFile(t *testing.T) {
	envFile := migratedDatabase(t)
	path := filepath.Join(t.TempDir(), "not-a-dump.sql")
	require.NoError(t, os.WriteFile(path, []byte("-- nothing to see\n"), 0o644))

	_, err := runDBImportCmd(t, "", "--env-file="+envFile, path)
	require.ErrorIs(t, err, database.ErrNoCopyBlocks)
}

// A missing file must fail before anything is touched.
func TestDBImportRejectsMissingFile(t *testing.T) {
	envFile := migratedDatabase(t)

	_, err := runDBImportCmd(t, "", "--env-file="+envFile,
		filepath.Join(t.TempDir(), "absent.sql"))
	require.Error(t, err)
}

// --truncate is destructive, so a declined prompt must leave the database alone.
func TestDBImportTruncateDeclined(t *testing.T) {
	sourceEnv := migratedDatabase(t)
	_, err := runMigrateSeedCmd(t, "", "--env-file="+sourceEnv, "--force")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "dump.sql")
	exportTo(t, sourceEnv, path, "--data-only")

	targetEnv := migratedDatabase(t)

	previous := terminalCheck
	terminalCheck = func(*cli.Command) bool { return true }
	t.Cleanup(func() { terminalCheck = previous })

	out, err := runDBImportCmd(t, "n\n", "--env-file="+targetEnv, "--truncate", path)
	require.NoError(t, err)

	assert.Contains(t, out, "import cancelled")
	assert.Zero(t, countUsers(t, targetEnv))
}

// --force skips the prompt, which is what a script needs.
func TestDBImportTruncateForceSkipsPrompt(t *testing.T) {
	sourceEnv := migratedDatabase(t)
	_, err := runMigrateSeedCmd(t, "", "--env-file="+sourceEnv, "--force")
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "dump.sql")
	exportTo(t, sourceEnv, path, "--data-only")

	targetEnv := migratedDatabase(t)

	// The terminal check is armed to prompt, so only --force can get past it.
	previous := terminalCheck
	terminalCheck = func(*cli.Command) bool { return true }
	t.Cleanup(func() { terminalCheck = previous })

	out, err := runDBImportCmd(t, "", "--env-file="+targetEnv, "--truncate", "--force", path)
	require.NoError(t, err)

	assert.NotContains(t, out, "import cancelled")
	assert.Equal(t, 1, countUsers(t, targetEnv))
}
