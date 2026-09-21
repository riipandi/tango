package main

import (
	"fmt"
	"io"
	"time"

	"github.com/briandowns/spinner"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/printext"
)

// spinnerDelay is how fast the indicator animates. The braille set has ten
// frames, so a whole turn takes about a second: fast enough to read as activity,
// slow enough not to flicker.
const spinnerDelay = 100 * time.Millisecond

// spinnerChars is the braille set. It is one column wide and never changes
// width, so the suffix beside it cannot shift.
var spinnerChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// newSpinner returns a spinner bound to w, or nil when w is not a terminal.
//
// The animation redraws its line with carriage returns, so it is only drawn
// where a line can be redrawn: redirected to a file or a pipe it would fill the
// log with control codes. A nil spinner draws nothing, which is what every
// method below checks for.
func newSpinner(w io.Writer) *spinner.Spinner {
	if !printext.IsTerminal(w) {
		return nil
	}
	return spinner.New(spinnerChars, spinnerDelay,
		spinner.WithWriter(w),
		spinner.WithHiddenCursor(true),
		// Only the glyph is coloured. The suffix stays plain because the spinner
		// measures its own output to erase it, and a colour code would be
		// counted as visible text.
		spinner.WithColor("yellow"),
	)
}

// tableSpinner shows that a dump or a restore is running, and which table it is
// on. A spinner is used rather than a progress bar because the number of rows is
// not known before a table has been read: a bar would have to be sized from a
// second query, and it would still stall for the whole of a large table. What a
// reader needs here is proof the command is alive and the name of the work in
// flight.
type tableSpinner struct {
	spinner *spinner.Spinner
}

// withSpinner returns the spinner a dump or a restore should use.
func withSpinner(w io.Writer) *tableSpinner {
	return &tableSpinner{spinner: newSpinner(w)}
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

// progressIndent marks a line that reports one step inside a run, so a reader
// can tell progress from the summary that closes it.
const progressIndent = "  "

// reportTarget announces the database a command works on, followed by a blank
// line. It is used where progress lines follow, so the target stays separate
// from the run beneath it.
func reportTarget(p printext.Palette, dsn string) error {
	target := postgresTarget(dsn)
	if target == "" {
		return nil
	}
	return p.Printf("%s %s\n\n", p.Dim("database:"), p.Dim(target))
}

// reporter renders a migration run while it happens.
//
// goose reports each migration through the Progress callback, so the run is
// visible as it happens rather than appearing all at once at the end. A
// migration that is still running is shown by the spinner, because a line for it
// would be overwritten on a terminal and lost in a log; a migration that has
// finished becomes a line that stays.
type reporter struct {
	p       printext.Palette
	started time.Time
	spin    *spinner.Spinner
	// err is the first write failure. A progress callback cannot return one, so
	// it is held until the command can report it.
	err error
}

// newReporter starts timing a run.
func newReporter(p printext.Palette) *reporter {
	return &reporter{p: p, started: time.Now(), spin: newSpinner(p.Writer())}
}

// progress is the callback handed to the migrator.
func (r *reporter) progress(event database.ProgressEvent) {
	if r.err != nil {
		return
	}
	if event.State == database.ProgressStarted {
		r.begin(event)
		return
	}
	r.stop()
	r.err = r.p.Printf("%s%s %s (%s)\n",
		progressIndent,
		event.Name,
		r.p.Paint(stateAttribute(event.State), string(event.State)),
		r.p.Dim(printext.Duration(event.Duration)))
}

// begin names the migration now in flight and starts the animation.
func (r *reporter) begin(event database.ProgressEvent) {
	if r.spin == nil {
		return
	}
	r.spin.Suffix = " " + event.Name
	if !r.spin.Active() {
		r.spin.Start()
	}
}

// stop ends the animation. Stopping erases the line and leaves the cursor at
// column zero, so the next line written lands where the spinner was.
func (r *reporter) stop() {
	if r.spin != nil {
		r.spin.Stop()
	}
}

// failed reports a write failure that happened while the run was in progress,
// and clears the spinner first so a failed run does not leave a hidden cursor.
func (r *reporter) failed() error {
	r.stop()
	return r.err
}

// elapsed is how long the command has been running.
func (r *reporter) elapsed() time.Duration { return time.Since(r.started) }

// stateAttribute maps a migration state to its colour. A state that is not a
// plain success is a warning rather than a failure: an empty migration or a
// rollback both did what was asked.
func stateAttribute(state database.ProgressState) printext.Colour {
	if state == database.ProgressApplied {
		return printext.Green
	}
	return printext.Yellow
}

// printSummary closes a report with what happened and how long it took. The
// duration is omitted when nothing was measured.
func printSummary(p printext.Palette, count int, verb, noun string, elapsed time.Duration) error {
	head := fmt.Sprintf("%d %s ", count, printext.Plural(count, noun))
	if elapsed > 0 {
		return p.Printf("\n%s%s %s\n", head, p.Paint(verbAttribute(verb), verb),
			p.Dim("in "+printext.Duration(elapsed)))
	}
	return p.Printf("\n%s%s\n", head, p.Paint(verbAttribute(verb), verb))
}

// verbAttribute maps the verb that closes a report to its colour: work that
// landed is a success, and anything else is a state to notice.
func verbAttribute(verb string) printext.Colour {
	if verb == "applied" {
		return printext.Green
	}
	return printext.Yellow
}
