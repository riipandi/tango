package health

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
)

// WriteText writes a human-readable report of a result.
//
// The shape is a flat list, one fact per line, so it greps and pipes without
// column padding to strip:
//
//	name: tango
//	version: 0.0.0
//	status: healthy
//	duration: 1.234 ms
//	checks: 2 up, 0 down
//
//	postgres: up (localhost:5432/postgres)
//	storage: up (/srv/storage)
//
// Every line is "<name>: <status>[ optional][ (<target>)][: <error>]", with one
// line per check in check-name order. A failing check carries its error on the
// same line, so `grep ': down'` finds every problem. Every failing check is
// reported, not just the first.
//
// Per-check durations and timestamps are deliberately absent: they are per-run
// numbers that answer no question a reader has. The JSON form keeps them for a
// machine that measures them.
func WriteText(w io.Writer, result Result) error {
	if err := writeSummary(w, result); err != nil {
		return err
	}
	if len(result.Details) == 0 {
		return nil
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return writeDetails(w, result)
}

// writeSummary writes the identity and aggregate lines.
func writeSummary(w io.Writer, result Result) error {
	if err := writeInfo(w, result.Info); err != nil {
		return err
	}

	lines := [][2]string{
		{"status", string(result.Status)},
		{"duration", duration(result.Duration)},
		{"checks", checkCounts(result.Details)},
	}
	for _, line := range lines {
		if _, err := fmt.Fprintf(w, "%s: %s\n", line[0], line[1]); err != nil {
			return err
		}
	}
	return nil
}

// writeInfo writes the info map as one "key: value" line per entry, in key
// order, so the output does not depend on map iteration order.
func writeInfo(w io.Writer, info map[string]string) error {
	for _, key := range sortedKeys(info) {
		if _, err := fmt.Fprintf(w, "%s: %s\n", key, info[key]); err != nil {
			return err
		}
	}
	return nil
}

// writeDetails writes one line per check, in check-name order.
func writeDetails(w io.Writer, result Result) error {
	for _, name := range sortedNames(result.Details) {
		if _, err := fmt.Fprintln(w, checkLine(result.Details[name])); err != nil {
			return err
		}
	}
	return nil
}

// checkLine renders one check as a single line. Optional parts are omitted
// rather than left blank, so a healthy check stays short and a failure keeps
// everything needed to act on it.
func checkLine(detail CheckResult) string {
	var line strings.Builder
	line.WriteString(detail.Name)
	line.WriteString(": ")
	line.WriteString(string(detail.Status))
	if detail.Optional {
		line.WriteString(" optional")
	}
	if detail.Target != "" {
		line.WriteString(" (")
		line.WriteString(detail.Target)
		line.WriteString(")")
	}
	if detail.Error != "" {
		line.WriteString(": ")
		line.WriteString(detail.Error)
	}
	return line.String()
}

// checkCounts summarizes the details as "1 up, 0 down", appending the optional
// count when there is one. Counts go through go-humanize so a large number stays
// readable.
func checkCounts(details map[string]CheckResult) string {
	var up, down, optional int
	for _, detail := range details {
		if detail.Optional {
			optional++
		}
		if detail.Status == StatusDown {
			down++
			continue
		}
		up++
	}

	summary := fmt.Sprintf("%s up, %s down", humanize.Comma(int64(up)), humanize.Comma(int64(down)))
	if optional > 0 {
		summary += fmt.Sprintf(", %s optional", humanize.Comma(int64(optional)))
	}
	return summary
}

// duration renders a duration for a human. go-humanize scales the unit, so a
// fast check reads as "235 µs" rather than "0s".
func duration(d time.Duration) string {
	return humanize.SI(d.Seconds(), "s")
}

// WriteShort writes only the aggregated status, one word on one line, so a
// shell script or a probe can read it without parsing anything:
//
//	healthy
//
// It prints the same word the JSON status field carries.
func WriteShort(w io.Writer, result Result) error {
	_, err := fmt.Fprintln(w, result.Status)
	return err
}

// Message describes a result in one line for a log entry or an API message. It
// names the failing checks, because a bare status does not say what to look at.
func Message(result Result) string {
	failed := result.Failed()
	if len(failed) == 0 {
		return fmt.Sprintf("%s (%s)", result.Status, duration(result.Duration))
	}
	return fmt.Sprintf("%s: %s is down", result.Status, strings.Join(failed, ", "))
}
