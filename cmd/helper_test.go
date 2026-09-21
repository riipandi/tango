//go:build debug

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/printext"
)

// plain is the palette a test uses. A bytes.Buffer is never a terminal, so the
// palette adds no colour and the expected strings stay readable.
func plain(w *bytes.Buffer) printext.Palette { return printext.NewPalette(w) }

// A summary is a labelled block, so the values line up in a column and a reader
// scans labels instead of parsing a sentence.
func TestPrintFieldsAlignsValues(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printFields(plain(&out), []field{
		{label: "written", value: "/tmp/dump.sql"},
		{label: "schema", value: "324 statements"},
		{label: "duration", value: "84 ms"},
	}))

	assert.Equal(t, "written:   /tmp/dump.sql\n"+
		"schema:    324 statements\n"+
		"duration:  84 ms\n", out.String())
}

// The export summary reports what was written and where, and omits a section it
// did not produce rather than printing a zero.
func TestPrintExportSummary(t *testing.T) {
	var out bytes.Buffer
	err := printExportSummary(plain(&out), "/tmp/dump.sql", database.DumpStats{
		Statements: 12,
		Tables:     3,
		Rows:       7,
	}, 1500*time.Millisecond)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "written:   /tmp/dump.sql")
	assert.Contains(t, out.String(), "schema:    12 statements")
	assert.Contains(t, out.String(), "data:      3 tables, 7 rows")
	assert.Contains(t, out.String(), "duration:  1.5 s")
}

// A data-only dump has no schema section, and a schema-only dump has no data
// section. Neither may report a zero.
func TestPrintExportSummaryOmitsAbsentSections(t *testing.T) {
	var dataOnly bytes.Buffer
	require.NoError(t, printExportSummary(plain(&dataOnly), "dump.sql",
		database.DumpStats{Tables: 1, Rows: 2}, time.Second))
	assert.NotContains(t, dataOnly.String(), "schema:")
	assert.Contains(t, dataOnly.String(), "data:")

	var schemaOnly bytes.Buffer
	require.NoError(t, printExportSummary(plain(&schemaOnly), "dump.sql",
		database.DumpStats{Statements: 5}, time.Second))
	assert.Contains(t, schemaOnly.String(), "schema:")
	assert.NotContains(t, schemaOnly.String(), "data:")
}

// The import summary names the file it read.
func TestPrintImportSummary(t *testing.T) {
	var out bytes.Buffer
	err := printImportSummary(plain(&out), "dump.sql",
		database.DumpStats{Tables: 40, Rows: 2}, 20*time.Millisecond)
	require.NoError(t, err)

	assert.Contains(t, out.String(), "loaded:    dump.sql")
	assert.Contains(t, out.String(), "data:      40 tables, 2 rows")
	assert.Contains(t, out.String(), "duration:  20 ms")
}

// The spinner only draws on a terminal, so a redirected run writes nothing and
// the report stays the only output.
func TestSpinnerStaysSilentOffTerminal(t *testing.T) {
	var out bytes.Buffer
	progress := withSpinner(&out)

	progress.step(database.TableProgress{Table: database.Table{Schema: "public", Name: "users"}})
	progress.finish()

	assert.Empty(t, out.String())
}

// The target opens the report block and the result closes it. Both calls pad
// their labels to the same width, so the block stays aligned across the two
// writes and the credentials never appear.
func TestPrintDatabaseAlignsWithTheSummary(t *testing.T) {
	var out bytes.Buffer
	p := plain(&out)
	require.NoError(t, printDatabase(p, "postgres://user:secret@db.example.com:5432/app"))
	require.NoError(t, printExportSummary(p, "dump.sql", database.DumpStats{
		Statements: 3,
	}, time.Second))

	assert.Equal(t, "database:  db.example.com:5432/app\n"+
		"written:   dump.sql\n"+
		"schema:    3 statements\n"+
		"duration:  1 s\n", out.String())
	assert.NotContains(t, out.String(), "secret")
}

// A DSN that cannot be parsed prints no target line rather than a broken one.
func TestPrintDatabaseSkipsUnparsableDSN(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printDatabase(plain(&out), "not a dsn"))
	assert.Empty(t, out.String())
}

// The target line names the database without its credentials, so a report can
// be pasted into a ticket.
func TestReportTargetOmitsCredentials(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, reportTarget(plain(&out),
		"postgres://user:secret@db.example.com:5432/tango?sslmode=disable"))

	assert.Equal(t, "database: db.example.com:5432/tango\n\n", out.String())
	assert.NotContains(t, out.String(), "secret")
}

// A DSN that cannot be parsed must not stop the command; the report simply has
// no target line.
func TestReportTargetSkipsUnparsableDSN(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, reportTarget(plain(&out), "not a dsn"))

	assert.Empty(t, out.String())
}

// Progress lines are indented and summaries are not, so a summary can be grepped
// at the start of a line.
func TestReporterIndentsProgressOnly(t *testing.T) {
	var out bytes.Buffer
	p := plain(&out)
	report := newReporter(p)

	report.progress(database.ProgressEvent{
		Version:   2,
		Name:      "00002_create_identity_tables.sql",
		Direction: "up",
		State:     database.ProgressApplied,
		Duration:  37 * time.Millisecond,
	})
	require.NoError(t, report.failed())

	assert.Equal(t, "  00002_create_identity_tables.sql applied (37 ms)\n", out.String())

	out.Reset()
	require.NoError(t, printSummary(p, 1, "applied", "migration", 100*time.Millisecond))

	line := strings.TrimSuffix(out.String(), "\n")
	line = strings.TrimPrefix(line, "\n")
	assert.True(t, strings.HasPrefix(line, "1 migration applied in "), "summary must start at column zero: %q", line)
}

// A started event starts the spinner and draws no line of its own: a line for a
// migration that is still running would be overwritten on a terminal and lost in
// a log.
func TestReporterIgnoresStartedEvents(t *testing.T) {
	var out bytes.Buffer
	report := newReporter(plain(&out))

	report.progress(database.ProgressEvent{
		Version:   1,
		Name:      "00001_initialize_schema.sql",
		Direction: "up",
		State:     database.ProgressStarted,
	})

	require.NoError(t, report.failed())
	assert.Empty(t, out.String(), "a redirected run must not draw the spinner")
}

// A write failure cannot be returned from the progress callback, so it must be
// held and surfaced when the command can report it.
func TestReporterHoldsWriteFailure(t *testing.T) {
	report := newReporter(printext.NewPalette(failingWriter{}))

	report.progress(database.ProgressEvent{
		Version:   1,
		Name:      "00001_initialize_schema.sql",
		Direction: "up",
		State:     database.ProgressApplied,
		Duration:  time.Millisecond,
	})

	require.ErrorIs(t, report.failed(), assert.AnError)
}

// failingWriter rejects every write.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, assert.AnError }
