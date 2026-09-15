package jobs

import "time"

// now is replaceable so tests can control time.
var now = func() time.Time {
	return time.Now().UTC()
}
