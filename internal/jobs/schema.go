// Package jobs owns the asynchronous task types that run on the built-in
// queue: transactional email and recurring maintenance. Payload types and
// queue tuning live here; the processors are injected by the composition
// root, so this package never imports a domain module. The webhook
// delivery task and its tuning live in modules/webhook, which owns the
// payload and the processor.
package jobs

import (
	"time"

	"github.com/riipandi/tango/internal/queue"
)

// Queue names. Every task type declares exactly one; the name is the
// registry key the queue uses, so it must stay stable across releases.
const (
	EmailQueue       = "email"
	MaintenanceQueue = "maintenance"
)

// Delivery tuning.
const (
	// EmailMaxAttempts covers relay hiccups; SMTP failures are usually
	// permanent after a handful of tries.
	EmailMaxAttempts = 5
	// EmailTimeout bounds one SMTP transaction, delivery plus retry hook.
	EmailTimeout = 45 * time.Second
	// EmailBackoff is the wait between delivery attempts.
	EmailBackoff = time.Minute

	// MaintenanceTimeout bounds a recurring job; token cleanup and log
	// pruning are bulk deletes and may take seconds.
	MaintenanceTimeout = 10 * time.Minute
	// MaintenanceBackoff is the wait before a failed maintenance run
	// retries.
	MaintenanceBackoff = time.Minute
	// MaintenanceJitter spreads recurring jobs across instances so two
	// deployments do not run the same prune at the same second.
	MaintenanceJitter = 5 * time.Minute
)

// EmailTask delivers one transactional email. The template name
// resolves inside internal/mailer (e.g. "email-verification"), and Data
// fills the template's .Data tree.
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

// RecurringTask is a self re-enqueueing maintenance job: every run
// schedules the next one at now + Interval (+ jitter). The task carries
// no state, so a duplicate schedule only causes a redundant idempotent
// run. The interval travels in seconds because encoding/json/v2 has no
// representation for time.Duration.
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
