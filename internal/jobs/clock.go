package jobs

import "time"

// now returns the current time in a form the recurring jobs can reason
// about; tests override it to move the clock instead of waiting for
// wall time to pass. Mirrors the seam the queue package (internal/queue)
// uses for its dispatcher.
var now = func() time.Time {
	return time.Now().UTC()
}
