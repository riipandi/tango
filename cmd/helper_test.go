//go:build debug

package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A summary is a labelled block, so the values line up in a column and a reader
// scans labels instead of parsing a sentence.
func TestPrintFieldsAlignsValues(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, printFields(&out, []field{
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
	err := printExportSummary(&out, "/tmp/dump.sql", database.DumpStats{
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
	require.NoError(t, printExportSummary(&dataOnly, "dump.sql",
		database.DumpStats{Tables: 1, Rows: 2}, time.Second))
	assert.NotContains(t, dataOnly.String(), "schema:")
	assert.Contains(t, dataOnly.String(), "data:")

	var schemaOnly bytes.Buffer
	require.NoError(t, printExportSummary(&schemaOnly, "dump.sql",
		database.DumpStats{Statements: 5}, time.Second))
	assert.Contains(t, schemaOnly.String(), "schema:")
	assert.NotContains(t, schemaOnly.String(), "data:")
}

// The import summary names the file it read.
func TestPrintImportSummary(t *testing.T) {
	var out bytes.Buffer
	err := printImportSummary(&out, "dump.sql",
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
	require.NoError(t, printDatabase(&out, "postgres://user:secret@db.example.com:5432/app"))
	require.NoError(t, printExportSummary(&out, "dump.sql", database.DumpStats{
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
	require.NoError(t, printDatabase(&out, "not a dsn"))
	assert.Empty(t, out.String())
}

// A report never says "1 migrations", so the singular form must be chosen by
// the count.
func TestPlural(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{count: 0, want: "migrations"},
		{count: 1, want: "migration"},
		{count: 2, want: "migrations"},
		{count: 9, want: "migrations"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, plural(tt.count, "migration"))
		})
	}
}

// Durations must be humanized, not raw nanoseconds, and capped at a precision a
// reader can act on.
func TestHumanDuration(t *testing.T) {
	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "microseconds", in: 250 * time.Microsecond, want: "250 µs"},
		{name: "milliseconds", in: 27608625 * time.Nanosecond, want: "27.608 ms"},
		{name: "fractional second", in: 1500 * time.Millisecond, want: "1.5 s"},
		{name: "seconds", in: 12 * time.Second, want: "12 s"},
		{name: "zero", in: 0, want: "0 s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, humanDuration(tt.in))
		})
	}
}

// The target line names the database without its credentials, so a report can
// be pasted into a ticket.
func TestReportTargetOmitsCredentials(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, reportTarget(&out, "postgres://user:secret@db.example.com:5432/tango?sslmode=disable"))

	assert.Equal(t, "database: db.example.com:5432/tango\n\n", out.String())
	assert.NotContains(t, out.String(), "secret")
}

// A DSN that cannot be parsed must not stop the command; the report simply has
// no target line.
func TestReportTargetSkipsUnparsableDSN(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, reportTarget(&out, "not a dsn"))

	assert.Empty(t, out.String())
}

// Progress lines are indented and summaries are not, so a summary can be grepped
// at the start of a line.
func TestReporterIndentsProgressOnly(t *testing.T) {
	var out bytes.Buffer
	report := newReporter(&out)

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
	require.NoError(t, printSummary(&out, 1, "applied", "migration", 100*time.Millisecond))

	line := strings.TrimSuffix(out.String(), "\n")
	line = strings.TrimPrefix(line, "\n")
	assert.True(t, strings.HasPrefix(line, "1 migration applied in "), "summary must start at column zero: %q", line)
}

// A started event draws nothing: the line for a migration that is still running
// would be overwritten on a terminal and lost in a log.
func TestReporterIgnoresStartedEvents(t *testing.T) {
	var out bytes.Buffer
	report := newReporter(&out)

	report.progress(database.ProgressEvent{
		Version:   1,
		Name:      "00001_initialize_schema.sql",
		Direction: "up",
		State:     database.ProgressStarted,
	})

	require.NoError(t, report.failed())
	assert.Empty(t, out.String())
}

// A write failure cannot be returned from the progress callback, so it must be
// held and surfaced when the command can report it.
func TestReporterHoldsWriteFailure(t *testing.T) {
	report := newReporter(failingWriter{})

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
