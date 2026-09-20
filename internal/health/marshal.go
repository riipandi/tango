package health

import (
	"encoding/json/v2"
	"time"
)

// This file defines the wire form of a result. Both surfaces publish it: the
// CLI prints it under --json and the REST handler sends it as the response
// payload, so the two never drift.
//
// Two choices differ from a plain struct dump:
//
//   - Durations are milliseconds. time.Duration marshals as nanoseconds, which
//     is unreadable in a probe response.
//   - Details are an ordered array, not a map. A map has no order, so a diff of
//     two responses would churn and a human would read a different order each
//     time. The order is by check name.

// jsonCheckResult is the wire form of CheckResult.
type jsonCheckResult struct {
	Name       string    `json:"name"`
	Status     Status    `json:"status"`
	Error      string    `json:"error,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
	DurationMS float64   `json:"duration_ms"`
	Optional   bool      `json:"optional,omitzero"`
}

// jsonResult is the wire form of Result.
type jsonResult struct {
	Status     GlobalStatus      `json:"status"`
	Details    []jsonCheckResult `json:"details"`
	DurationMS float64           `json:"duration_ms"`
	Info       map[string]string `json:"info,omitempty"`
}

// MarshalJSON implements json.Marshaler.
func (r CheckResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.wire())
}

// MarshalJSON implements json.Marshaler.
func (r Result) MarshalJSON() ([]byte, error) {
	details := make([]jsonCheckResult, 0, len(r.Details))
	for _, name := range sortedNames(r.Details) {
		details = append(details, r.Details[name].wire())
	}
	return json.Marshal(jsonResult{
		Status:     r.Status,
		Details:    details,
		DurationMS: milliseconds(r.Duration),
		Info:       r.Info,
	})
}

// wire converts a check result to its published form.
func (r CheckResult) wire() jsonCheckResult {
	return jsonCheckResult{
		Name:       r.Name,
		Status:     r.Status,
		Error:      r.Error,
		Timestamp:  r.Timestamp,
		DurationMS: milliseconds(r.Duration),
		Optional:   r.Optional,
	}
}

// milliseconds renders a duration as fractional milliseconds.
func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
