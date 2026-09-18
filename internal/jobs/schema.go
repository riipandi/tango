// Package jobs defines queued email and recurring maintenance tasks.
package jobs

import (
	"time"

	"github.com/riipandi/tango/internal/queue"
)

// Queue names used by the task types.
const (
	EmailQueue       = "email"
	MaintenanceQueue = "maintenance"
)

// Queue tuning.
const (
	// EmailMaxAttempts is the maximum number of delivery attempts.
	EmailMaxAttempts = 5
	// EmailTimeout bounds one SMTP transaction.
	EmailTimeout = 45 * time.Second
	// EmailBackoff is the wait between delivery attempts.
	EmailBackoff = time.Minute

	// MaintenanceTimeout bounds one recurring job.
	MaintenanceTimeout = 10 * time.Minute
	// MaintenanceBackoff is the wait before a failed maintenance run
	// retries.
	MaintenanceBackoff = time.Minute
	// MaintenanceJitter spreads recurring jobs across instances.
	MaintenanceJitter = 5 * time.Minute
)

// EmailTask describes one transactional email.
type EmailTask struct {
	To       string         `json:"to"`
	Subject  string         `json:"subject"`
	Template string         `json:"template"`
	Data     map[string]any `json:"data,omitzero"`
}

// Config implements queue.Task.
func (EmailTask) Config() queue.QueueConfig {
	return queue.QueueConfig{
		Name:        EmailQueue,
		MaxAttempts: EmailMaxAttempts,
		Timeout:     EmailTimeout,
		Backoff:     EmailBackoff,
	}
}

// RecurringTask describes one self-scheduling maintenance run.
type RecurringTask struct {
	Job             string `json:"job"`
	IntervalSeconds int64  `json:"interval_seconds"`
}

// Interval returns the carried interval as a duration.
func (t RecurringTask) Interval() time.Duration {
	return time.Duration(t.IntervalSeconds) * time.Second
}

// Config implements queue.Task.
func (RecurringTask) Config() queue.QueueConfig {
	return queue.QueueConfig{
		Name:        MaintenanceQueue,
		MaxAttempts: 1,
		Timeout:     MaintenanceTimeout,
		Backoff:     MaintenanceBackoff,
	}
}
