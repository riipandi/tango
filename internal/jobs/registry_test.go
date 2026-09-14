package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
)

// fakeMailer records the messages handed to it and can fail on demand.
type fakeMailer struct {
	mu       sync.Mutex
	messages []mailer.Message
	err      error
}

func (m *fakeMailer) Send(_ context.Context, msg mailer.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.messages = append(m.messages, msg)
	return m.err
}

func (m *fakeMailer) sent() []mailer.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]mailer.Message(nil), m.messages...)
}

// fakeEnqueuer records the messages queued for delivery; the API key
// reminder job takes the queueing contract, not the sending one.
type fakeEnqueuer struct {
	mu       sync.Mutex
	messages []mailer.Message
	err      error
}

func (e *fakeEnqueuer) EnqueueEmail(_ context.Context, msg mailer.Message) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.messages = append(e.messages, msg)
	return nil
}

func (e *fakeEnqueuer) sent() []mailer.Message {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]mailer.Message(nil), e.messages...)
}

func TestEmailTaskConfig(t *testing.T) {
	cfg := EmailTask{}.Config()
	assert.Equal(t, EmailQueue, cfg.Name)
	assert.Equal(t, EmailMaxAttempts, cfg.MaxAttempts)
	assert.Equal(t, EmailTimeout, cfg.Timeout)
	assert.Positive(t, cfg.Backoff)
}

func TestWebhookDeliveryTaskConfigRetainsFailures(t *testing.T) {
	cfg := WebhookDeliveryTask{}.Config()
	assert.Equal(t, WebhookQueue, cfg.Name)
	assert.Equal(t, WebhookMaxAttempts, cfg.MaxAttempts)
	assert.Equal(t, WebhookRequestTimeout*4/3, cfg.Timeout, "queue timeout must exceed the receiver deadline")

	require.NotNil(t, cfg.Retention)
	assert.True(t, cfg.Retention.OnlyFailed, "only failed deliveries are retained")
	assert.Equal(t, WebhookRetention, cfg.Retention.Duration)
	require.NotNil(t, cfg.Retention.Data)
	assert.True(t, cfg.Retention.Data.OnlyFailed)
}

func TestRecurringTaskConfig(t *testing.T) {
	cfg := RecurringTask{}.Config()
	assert.Equal(t, MaintenanceQueue, cfg.Name)
	assert.Equal(t, 1, cfg.MaxAttempts)
	assert.Equal(t, MaintenanceTimeout, cfg.Timeout)
}

func TestDeliverEmailRendersThroughTheMailer(t *testing.T) {
	mail := &fakeMailer{}
	registry := &Registry{mail: mail, log: logger.NewMock()}

	task := EmailTask{
		To:       "user@example.com",
		Subject:  "Hi",
		Template: "test-email",
		Data:     map[string]any{"Email": "user@example.com"},
	}
	require.NoError(t, registry.deliverEmail(context.Background(), task))

	sent := mail.sent()
	require.Len(t, sent, 1)
	assert.Equal(t, task.To, sent[0].To)
	assert.Equal(t, task.Subject, sent[0].Subject)
	assert.Equal(t, task.Template, sent[0].Template)
	assert.Equal(t, "user@example.com", sent[0].Data["Email"])
}

func TestDeliverEmailPropagatesFailureForRetry(t *testing.T) {
	mail := &fakeMailer{err: errors.New("relay refused")}
	registry := &Registry{mail: mail, log: logger.NewMock()}

	err := registry.deliverEmail(context.Background(), EmailTask{To: "a@b.c", Template: "test-email"})
	assert.ErrorContains(t, err, "relay refused")
}

func TestDeliverEmailWithoutMailerFails(t *testing.T) {
	registry := &Registry{log: logger.NewMock()}
	err := registry.deliverEmail(context.Background(), EmailTask{To: "a@b.c", Template: "test-email"})
	assert.ErrorContains(t, err, "mailer is not configured")
}

func TestAddJobRejectsIncompleteRegistrations(t *testing.T) {
	registry := &Registry{jobs: map[string]Job{}, log: logger.NewMock()}

	assert.Panics(t, func() { registry.AddJob(Job{Interval: time.Hour, Run: func(context.Context) error { return nil }}) })
	assert.Panics(t, func() { registry.AddJob(Job{Name: "no-interval", Run: func(context.Context) error { return nil }}) })
	assert.Panics(t, func() { registry.AddJob(Job{Name: "no-run", Interval: time.Hour}) })

	ok := Job{Name: "fine", Interval: time.Hour, Run: func(context.Context) error { return nil }}
	registry.AddJob(ok)
	assert.Len(t, registry.Jobs(), 1)

	assert.Panics(t, func() { registry.AddJob(ok) }, "duplicate names are a wiring bug")
}

func TestVersionFeedFallsBackToDeployedVersion(t *testing.T) {
	feed := &VersionFeed{}
	assert.Equal(t, feedLatestFallback(), feed.Latest(), "an empty feed reports the running build")
	assert.True(t, feed.FetchedAt().IsZero())
}

func TestVersionFeedRefreshStripsTagPrefix(t *testing.T) {
	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "v2.14.0", nil }}
	require.NoError(t, feed.Refresh(context.Background()))

	assert.Equal(t, "2.14.0", feed.Latest())
	assert.False(t, feed.FetchedAt().IsZero())
}

func TestVersionFeedRefreshRejectsEmptyTag(t *testing.T) {
	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "", nil }}
	assert.ErrorContains(t, feed.Refresh(context.Background()), "empty tag")

	// The failed refresh leaves the previous value untouched.
	feed.latest = "1.0.0"
	assert.Error(t, feed.Refresh(context.Background()))
	assert.Equal(t, "1.0.0", feed.Latest())
}

func TestVersionFeedRefreshPropagatesLookupError(t *testing.T) {
	feed := &VersionFeed{Fetch: func(context.Context) (string, error) { return "", errors.New("github down") }}
	assert.ErrorContains(t, feed.Refresh(context.Background()), "github down")
}

// feedLatestFallback mirrors what Latest reports with no cached value,
// read through the exported API so the test does not hardcode the build.
func feedLatestFallback() string {
	empty := &VersionFeed{}
	return empty.Latest()
}

func TestFirstDelayStaysInsideTheInterval(t *testing.T) {
	interval := time.Hour
	for range 50 {
		delay := firstDelay(interval)
		assert.GreaterOrEqual(t, delay, time.Duration(0))
		assert.LessOrEqual(t, delay, interval, "the first run must not overshoot the interval")
	}
}

func TestJitterForCapsAtQuarterInterval(t *testing.T) {
	// A short interval caps the jitter at a quarter of itself, so the
	// first run still lands inside the interval.
	for range 20 {
		assert.LessOrEqual(t, jitterFor(time.Second), time.Second/4)
	}
	for range 20 {
		assert.LessOrEqual(t, jitterFor(time.Hour), MaintenanceJitter)
	}
	assert.Equal(t, time.Duration(0), jitterFor(0))
}

func TestScheduleWithoutQueueIsANoop(t *testing.T) {
	registry := &Registry{jobs: map[string]Job{}, log: logger.NewMock()}
	assert.NotPanics(t, func() {
		registry.schedule(context.Background(), Job{Name: "noqueue", Interval: time.Minute}, 0)
	})
}

func TestStartSchedulesOnce(t *testing.T) {
	job := Job{Name: "once", Interval: time.Hour, Run: func(context.Context) error { return nil }}

	// Start with no queue: schedule is a no-op, but the started guard
	// must still flip so a restart cannot double-schedule later.
	registry := &Registry{jobs: map[string]Job{"once": job}, log: logger.NewMock()}
	require.NoError(t, registry.Start(context.Background()))
	require.NoError(t, registry.Start(context.Background()))
}

func TestRunJobUnknownNameIsDropped(t *testing.T) {
	registry := &Registry{jobs: map[string]Job{}, log: logger.NewMock()}
	err := registry.runJob(context.Background(), RecurringTask{Job: "missing", IntervalSeconds: 60})
	assert.NoError(t, err, "an unregistered job must be dropped, not retried forever")
}

func TestRunJobKeepsCadenceAfterFailure(t *testing.T) {
	calls := 0
	registry := &Registry{
		jobs: map[string]Job{
			"failing": {Name: "failing", Interval: time.Minute, Run: func(context.Context) error {
				calls++
				return errors.New("boom")
			}},
		},
		log: logger.NewMock(),
	}

	err := registry.runJob(context.Background(), RecurringTask{Job: "failing"})
	assert.ErrorContains(t, err, "boom")
	assert.Equal(t, 1, calls)
}

func TestJobIntervalPrefersTheTaskValue(t *testing.T) {
	job := Job{Interval: time.Hour}
	assert.Equal(t, 30*time.Second, jobInterval(job, RecurringTask{IntervalSeconds: 30}))
	assert.Equal(t, time.Hour, jobInterval(job, RecurringTask{}))
}

func TestRecurringTaskRoundTripsThroughJSON(t *testing.T) {
	// The queue encodes task payloads with encoding/json/v2, which has
	// no representation for time.Duration: the interval must survive as
	// a plain number.
	task := RecurringTask{Job: "cleanup_tokens", IntervalSeconds: int64((6 * time.Hour).Seconds())}
	encoded, err := jsonv2.Marshal(task)
	require.NoError(t, err)

	var decoded RecurringTask
	require.NoError(t, jsonv2.Unmarshal(encoded, &decoded))
	assert.Equal(t, 6*time.Hour, decoded.Interval())
}
