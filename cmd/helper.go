package main

import (
	"fmt"
	"io"
	"os"
	"time"

	"github.com/briandowns/spinner"
	"github.com/dustin/go-humanize"
	"github.com/riipandi/tango/database"
	"golang.org/x/term"
)

// spinnerDelay is how fast the indicator animates. The braille set has ten
// frames, so a whole turn takes about a second: fast enough to read as activity,
// slow enough not to flicker.
const spinnerDelay = 100 * time.Millisecond

// spinnerChars is the braille set. It is one column wide and never changes
// width, so the suffix beside it cannot shift.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// tableSpinner shows that a dump or a restore is running, and which table it is
// on. A spinner is used rather than a progress bar because the number of rows is
// not known before a table has been read: a bar would have to be sized from a
// second query, and it would still stall for the whole of a large table. What a
// reader needs here is proof the command is alive and the name of the work in
// flight.
//
// The spinner is only drawn on a terminal. Redirected to a file or a pipe it
// would write carriage returns into the log, which is worse than nothing.
type tableSpinner struct {
	spinner *spinner.Spinner
}

// withSpinner returns the spinner a command should use, or one that draws
// nothing when the output is not a terminal.
func withSpinner(w io.Writer) *tableSpinner {
	if !isTerminalWriter(w) {
		return &tableSpinner{}
	}
	s := spinner.New(spinnerChars, spinnerDelay,
		spinner.WithWriter(w),
		spinner.WithHiddenCursor(true),
	)
	return &tableSpinner{spinner: s}
}

// step names the table now in flight. It starts the spinner on the first call,
// because the table is the only progress worth reporting and it is not known
// until the work starts.
func (t *tableSpinner) step(progress database.TableProgress) {
	if t == nil || t.spinner == nil {
		return
	}
	t.spinner.Suffix = " " + progress.Table.String()
	if !t.spinner.Active() {
		t.spinner.Start()
	}
}

// finish stops the spinner. Stopping erases the line and leaves the cursor at
// column zero, so the report that follows overwrites it instead of leaving a
// blank line behind. When the spinner never ran nothing was drawn and the blank
// line above the report is already the only separation.
func (t *tableSpinner) finish() {
	if t == nil || t.spinner == nil {
		return
	}
	t.spinner.Stop()
}

// isTerminalWriter reports whether w is a terminal, so progress output is only
// drawn where it can be redrawn in place.
func isTerminalWriter(w io.Writer) bool {
	file, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}

// progressIndent marks a line that reports one step inside a run, so a reader
// can tell progress from the summary that closes it.
const progressIndent = "  "

// plural adds the plural suffix unless the count is one, so a report never
// reads "1 migration(s)".
func plural(count int, noun string) string {
	if count == 1 {
		return noun
	}
	return noun + "s"
}

// humanDuration renders a duration the way the health report does, so the CLI
// speaks one language: "28 ms", "1.5 s". Three decimals is the precision a
// reader can act on; the raw nanoseconds humanize would otherwise print are
// noise.
func humanDuration(d time.Duration) string {
	return humanize.SIWithDigits(d.Seconds(), 3, "s")
}

// reportTarget announces the database a command works on, followed by a blank
// line. It is used where progress lines follow, so the target stays separate
// from the run beneath it.
func reportTarget(w io.Writer, dsn string) error {
	target := postgresTarget(dsn)
	if target == "" {
		return nil
	}
	_, err := fmt.Fprintf(w, "database: %s\n\n", target)
	return err
}

// reporter renders a migration run while it happens.
//
// goose reports each finished migration through the Progress callback, so the
// report grows one line per migration instead of appearing all at once at the
// end. A long migration therefore leaves the previous ones visible while it
// runs, which a blank wait cannot.
type reporter struct {
	w       io.Writer
	started time.Time
	// err is the first write failure. A progress callback cannot return one, so
	// it is held until the command can report it.
	err error
}

// newReporter starts timing a run.
func newReporter(w io.Writer) *reporter {
	return &reporter{w: w, started: time.Now()}
}

// progress is the callback handed to the migrator. It draws the migrations that
// have finished; a migration that is still running is not drawn, because a
// terminal is not guaranteed and an overwritten line would be lost in a log.
func (r *reporter) progress(event database.ProgressEvent) {
	if r.err != nil || event.State == database.ProgressStarted {
		return
	}
	_, r.err = fmt.Fprintf(r.w, "%s%s %s (%s)\n",
		progressIndent, event.Name, event.State, humanDuration(event.Duration))
}

// failed reports a write failure that happened while the run was in progress.
func (r *reporter) failed() error { return r.err }

// elapsed is how long the command has been running.
func (r *reporter) elapsed() time.Duration { return time.Since(r.started) }

// printSummary closes a report with what happened and how long it took. The
// duration is omitted when nothing was measured.
func printSummary(w io.Writer, count int, verb, noun string, elapsed time.Duration) error {
	if elapsed > 0 {
		_, err := fmt.Fprintf(w, "\n%d %s %s in %s\n", count, plural(count, noun), verb, humanDuration(elapsed))
		return err
	}
	_, err := fmt.Fprintf(w, "\n%d %s %s\n", count, plural(count, noun), verb)
	return err
}
