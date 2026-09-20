package main

import (
	"fmt"
	"io"
	"time"

	"github.com/dustin/go-humanize"

	"github.com/riipandi/tango/database"
)

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

// reportTarget announces the database a command works on. It is printed before
// any work starts, so a run against the wrong server is visible immediately
// instead of after the fact. The credentials are never printed.
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
