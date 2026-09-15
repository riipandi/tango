// Package jobs owns the asynchronous task types that run on the antree
// queue: transactional email, webhook deliveries, and recurring
// maintenance. Payload types and queue tuning live here; the processors
// are injected by the composition root, so this package never imports a
// domain module.
package jobs

import (
	"time"

	"github.com/riipandi/tango/internal/antree"
)

// Queue names. Every task type declares exactly one; the name is the
// registry key antree uses, so it must stay stable across releases.
const (
	EmailQueue       = "email"
	WebhookQueue     = "webhook"
	MaintenanceQueue = "maintenance"
)

// Delivery tuning. Webhook attempts are bounded by the queue's own
// backoff, so the values here stay deliberately small: a failing
// receiver must not stall the worker pool for minutes.
const (
	// EmailMaxAttempts covers relay hiccups; SMTP failures are usually
	// permanent after a handful of tries.
	EmailMaxAttempts = 5
	// EmailTimeout bounds one SMTP transaction, delivery plus retry hook.
	EmailTimeout = 45 * time.Second
	// EmailBackoff is the wait between delivery attempts.
	EmailBackoff = time.Minute

	// WebhookMaxAttempts is the retry budget for one signed delivery.
	WebhookMaxAttempts = 5
	// WebhookTimeout bounds the outbound request; the spec fixes 30s,
	// the extra 10s absorbs connection setup and TLS.
	WebhookTimeout = 40 * time.Second
	// WebhookRequestTimeout is the receiver-facing deadline.
	WebhookRequestTimeout = 30 * time.Second
	// WebhookBackoff is the wait between delivery attempts.
	WebhookBackoff = 30 * time.Second
	// WebhookRetention keeps completed delivery tasks for a week so
	// failed payloads stay inspectable; the webhook_logs row is the
	// primary record and is pruned separately.
	WebhookRetention = 7 * 24 * time.Hour

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

// Config implements antree.Task.
func (EmailTask) Config() antree.QueueConfig {
	return antree.QueueConfig{
		Name:        EmailQueue,
		MaxAttempts: EmailMaxAttempts,
		Timeout:     EmailTimeout,
		Backoff:     EmailBackoff,
	}
}

// WebhookDeliveryTask delivers one recorded event to one endpoint. The
// log row is the outbox record: it is written in the same transaction
// that enqueues this task, so a rolled back event never delivers.
type WebhookDeliveryTask struct {
	// LogID identifies the webhook_logs row updated per attempt.
	LogID string `json:"log_id"`

	// WebhookID identifies the endpoint row.
	WebhookID string `json:"webhook_id"`

	// Event is the event name the endpoint subscribed to.
	Event string `json:"event"`

	// Payload is the event body; the delivery signs its canonical JSON
	// encoding byte for byte.
	Payload map[string]any `json:"payload,omitzero"`
}

// Config implements antree.Task.
func (WebhookDeliveryTask) Config() antree.QueueConfig {
	return antree.QueueConfig{
		Name:        WebhookQueue,
		MaxAttempts: WebhookMaxAttempts,
		Timeout:     WebhookTimeout,
		Backoff:     WebhookBackoff,
		Retention: &antree.Retention{
			Duration:   WebhookRetention,
			OnlyFailed: true,
			Data:       &antree.RetainData{OnlyFailed: true},
		},
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

// Config implements antree.Task.
func (RecurringTask) Config() antree.QueueConfig {
	return antree.QueueConfig{
		Name:        MaintenanceQueue,
		MaxAttempts: 1,
		Timeout:     MaintenanceTimeout,
		Backoff:     MaintenanceBackoff,
	}
}
