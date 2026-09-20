package health

import (
	"maps"
	"sync"
	"time"
)

// state holds what a Checker remembers between calls: the last result of each
// check, and the aggregated status last reported to a listener.
//
// It is separate from Checker so the locking rules live in one place: the
// result of every check is written by the goroutine that ran it and read by the
// caller that assembles the aggregate.
type state struct {
	mu sync.Mutex
	// results holds the last outcome per check name.
	results map[string]CheckResult
	// reportedStatus is the aggregated status last handed to a listener, and
	// hasReported distinguishes a real status from "never reported".
	reportedStatus GlobalStatus
	hasReported    bool
}

// newState returns an empty state sized for the configured checks.
func newState(checks int) state {
	return state{results: make(map[string]CheckResult, checks)}
}

// due returns the checks whose result is missing or older than ttl. A ttl of
// zero makes every check due, which is what disables the cache.
func (s *state) due(checks []Check, now time.Time, ttl time.Duration) []Check {
	s.mu.Lock()
	defer s.mu.Unlock()

	due := make([]Check, 0, len(checks))
	for _, check := range checks {
		result, ok := s.results[check.Name]
		if !ok || result.Timestamp.IsZero() || now.Sub(result.Timestamp) >= ttl {
			due = append(due, check)
		}
	}
	return due
}

// store records fresh outcomes, leaving every other entry untouched.
func (s *state) store(results map[string]CheckResult) {
	s.mu.Lock()
	defer s.mu.Unlock()

	maps.Copy(s.results, results)
}

// snapshot copies the current results, so the caller assembles the aggregate
// without holding the lock while it sorts and walks the map.
func (s *state) snapshot() map[string]CheckResult {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make(map[string]CheckResult, len(s.results))
	maps.Copy(out, s.results)
	return out
}

// recordStatus stores the aggregated status and reports whether it differs from
// the one reported before. The first call is not a change: there is nothing to
// compare against yet.
func (s *state) recordStatus(status GlobalStatus) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	changed = s.hasReported && s.reportedStatus != status
	s.reportedStatus = status
	s.hasReported = true
	return changed
}
