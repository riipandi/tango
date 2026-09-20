package health

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/dustin/go-humanize"
)

// WriteText writes a human-readable report of a result.
//
// The shape is fixed, so the output is stable between runs and a reader can
// rely on it: identity and aggregate lines as "key: value", then a blank line,
// then one row per check. Every failing check appears, not just the first.
//
//	name: tango
//	version: 0.0.0
//	status: up
//	duration: 1.234 ms
//	checks: 1 up, 0 down
//
//	CHECK     STATUS  DURATION  CHECKED  ERROR
//	postgres  up      235 µs    now
//
// The table is aligned with tabwriter, so columns stay readable as check names
// and errors grow. Durations and timestamps go through go-humanize, so a reader
// sees "1.234 ms" and "2 seconds ago" instead of nanoseconds and a raw
// timestamp. A caller that needs a machine format uses --json instead.
func WriteText(w io.Writer, result Result) error {
	if err := writeSummary(w, result); err != nil {
		return err
	}
	if len(result.Details) == 0 {
		return nil
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

// writeDetails writes the per-check table, one row per check in name order.
func writeDetails(w io.Writer, result Result) error {
	var table bytes.Buffer
	columns := tabwriter.NewWriter(&table, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(columns, "CHECK\tSTATUS\tDURATION\tCHECKED\tERROR"); err != nil {
		return err
	}
	for _, name := range sortedNames(result.Details) {
		detail := result.Details[name]
		status := string(detail.Status)
		if detail.Optional {
			status += " (optional)"
		}
		if _, err := fmt.Fprintf(columns, "%s\t%s\t%s\t%s\t%s\n",
			detail.Name, status, duration(detail.Duration), relative(detail.Timestamp), detail.Error); err != nil {
			return err
		}
	}
	if err := columns.Flush(); err != nil {
		return err
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	return writeTrimmed(w, table.String())
}

// writeTrimmed writes every line with its trailing spaces removed. tabwriter
// pads a column to align it, which leaves trailing spaces on the last cell of a
// row; they are noise in a diff and in a copied line.
func writeTrimmed(w io.Writer, content string) error {
	for _, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		if _, err := fmt.Fprintln(w, strings.TrimRight(line, " ")); err != nil {
			return err
		}
	}
	return nil
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

// relative renders a timestamp as the time since it was taken. A zero timestamp
// has no answer, so it is reported as never checked.
func relative(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return humanize.Time(t)
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
