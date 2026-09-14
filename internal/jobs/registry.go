package jobs

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/pkg/antree"
)

// Mailer is the delivery contract the email queue needs; internal/mailer
// implements it.
type Mailer interface {
	Send(ctx context.Context, msg mailer.Message) error
}

// Job is one recurring maintenance unit: a name plus its work.
type Job struct {
	// Name identifies the job in logs and in the queued payload. It
	// must stay unique per registry, because the execution looks the
	// job up by name.
	Name string

	// Interval is the delay between the end of one run and the start
	// of the next.
	Interval time.Duration

	// Run performs the work. A returned error makes the queue retry
	// per the maintenance queue's backoff before the next schedule.
	Run func(ctx context.Context) error
}

// Registry holds the process-wide queue registrations and recurring
// jobs. Queues are registered at build time; jobs are scheduled once
// by Start.
type Registry struct {
	queue *antree.Client
	mail  Mailer
	log   logger.Logger

	// jobs is populated at build time and read by the maintenance
	// processor, which runs on worker goroutines.
	jobs map[string]Job
	mu   sync.RWMutex

	// feed backs /api/version/latest; nil until wired.
	feed *VersionFeed

	// started guards the one-shot schedule at boot.
	started sync.Once
}

// NewRegistry registers the shared queues on the client: transactional
// email and recurring maintenance. The webhook delivery queue is
// declared in the same jobs package (its payload type lives here) but
// registered by the webhook module, which owns the processor.
func NewRegistry(queue *antree.Client, mail Mailer, log logger.Logger) *Registry {
	r := &Registry{
		queue: queue,
		mail:  mail,
		log:   log,
		jobs:  make(map[string]Job),
	}

	queue.Register(antree.NewQueue(func(ctx context.Context, task EmailTask) error {
		return r.deliverEmail(ctx, task)
	}))
	queue.Register(antree.NewQueue(func(ctx context.Context, task RecurringTask) error {
		return r.runJob(ctx, task)
	}))

	return r
}

// AddJob registers a recurring job. Panics on a duplicate name: the
// registry is a build-time table, so a collision is a wiring bug.
func (r *Registry) AddJob(job Job) {
	if job.Name == "" || job.Run == nil {
		panic("jobs: recurring job needs a name and a run function")
	}
	if job.Interval <= 0 {
		panic(fmt.Sprintf("jobs: recurring job %q needs a positive interval", job.Name))
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.jobs[job.Name]; exists {
		panic(fmt.Sprintf("jobs: recurring job %q already registered", job.Name))
	}
	r.jobs[job.Name] = job
}

// Jobs returns the registered recurring jobs.
func (r *Registry) Jobs() []Job {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Job, 0, len(r.jobs))
	for _, job := range r.jobs {
		out = append(out, job)
	}
	return out
}

// Start schedules every recurring job. Safe to call once; later calls
// are ignored, so a restart of the module does not double-schedule.
func (r *Registry) Start(ctx context.Context) error {
	r.started.Do(func() {
		for _, job := range r.Jobs() {
			r.schedule(ctx, job, firstDelay(job.Interval))
		}
	})
	return nil
}

// SetVersionFeed attaches the cached newest-release lookup so the
// transport layer can read it straight off the jobs module.
func (r *Registry) SetVersionFeed(feed *VersionFeed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.feed = feed
}

// Latest returns the cached newest release, falling back to the
// deployed build. Implements the transport latest-version contract.
func (r *Registry) Latest() string {
	r.mu.RLock()
	feed := r.feed
	r.mu.RUnlock()

	if feed == nil {
		return config.AppVersion
	}
	return feed.Latest()
}

// Stop implements the module lifecycle: queued work drains through the
// queue module, so this is a no-op.
func (*Registry) Stop(context.Context) error { return nil }

// Name implements the module contract.
func (*Registry) Name() string { return "jobs" }

// EnqueueEmail queues one transactional email. Errors here mean the
// task row was not written, which is a caller-visible failure (the
// HTTP request should surface it), unlike a later delivery failure.
func (r *Registry) EnqueueEmail(ctx context.Context, msg mailer.Message) error {
	if r.queue == nil {
		return fmt.Errorf("jobs: queue is not configured")
	}
	_, err := r.queue.Add(EmailTask{
		To:       msg.To,
		Subject:  msg.Subject,
		Template: msg.Template,
		Data:     msg.Data,
	}).Ctx(ctx).Save()
	return err
}

// deliverEmail renders and sends one queued message through the
// shared mailer. A non-nil error lets the queue retry.
func (r *Registry) deliverEmail(ctx context.Context, task EmailTask) error {
	if r.mail == nil {
		return fmt.Errorf("jobs: mailer is not configured")
	}
	return r.mail.Send(ctx, mailer.Message{
		To:       task.To,
		Subject:  task.Subject,
		Template: task.Template,
		Data:     task.Data,
	})
}

// runJob executes a recurring job and schedules its next run, whether
// the current run succeeded or not: a failing job must not silently
// stop recurring.
func (r *Registry) runJob(ctx context.Context, task RecurringTask) error {
	r.mu.RLock()
	job, ok := r.jobs[task.Job]
	r.mu.RUnlock()

	if !ok {
		r.log.Warn(fmt.Sprintf("jobs: recurring job %q is not registered, dropping", task.Job))
		return nil
	}

	runErr := job.Run(ctx)
	if runErr != nil {
		r.log.WithError(runErr).Warn(fmt.Sprintf("jobs: recurring job %q failed", job.Name))
	}

	// Schedule the next run before returning: the failure path must
	// keep the cadence.
	r.schedule(ctx, job, jobInterval(job, task))
	return runErr
}

// schedule enqueues one run of job after delay.
func (r *Registry) schedule(ctx context.Context, job Job, delay time.Duration) {
	if r.queue == nil {
		return
	}

	interval := job.Interval
	_, err := r.queue.Add(RecurringTask{
		Job:             job.Name,
		IntervalSeconds: int64(interval.Seconds()),
	}).
		Ctx(ctx).
		Wait(delay).
		Save()
	if err != nil {
		r.log.WithError(err).Error(fmt.Sprintf("jobs: failed to schedule %q", job.Name))
	}
}

// jobInterval keeps the interval the task carried, falling back to the
// registered value when a stale payload arrives after a config change.
func jobInterval(job Job, task RecurringTask) time.Duration {
	if carried := task.Interval(); carried > 0 {
		return carried
	}
	return job.Interval
}

// firstDelay spreads the initial runs of the whole registry so a
// restart does not fire every job at the same instant; each job's
// first run lands inside its own interval. Math/rand is deliberate:
// the value only spreads scheduling load and is not a security input.
func firstDelay(interval time.Duration) time.Duration {
	jitter := jitterFor(interval)
	return jitter + time.Duration(rand.Int64N(int64(interval/2)+1)) //nolint:gosec // scheduling jitter, not a secret
}

// jitterFor spreads recurring runs across instances without moving the
// cadence much: half the maintenance jitter, capped by the interval.
func jitterFor(interval time.Duration) time.Duration {
	jitter := MaintenanceJitter
	if jitter > interval/4 {
		jitter = interval / 4
	}
	if jitter <= 0 {
		return 0
	}
	//nolint:gosec // scheduling jitter, not a secret
	return time.Duration(rand.Int64N(int64(jitter) + 1))
}
