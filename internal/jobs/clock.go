package jobs

import "time"

// now returns the current time in a form the recurring jobs can reason
// about; tests override it to move the clock instead of waiting for
// wall time to pass. Mirrors the seam pkg/antree uses for its queue.
var now = func() time.Time {
	return time.Now().UTC()
}
