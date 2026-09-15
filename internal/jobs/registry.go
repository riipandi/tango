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
	"github.com/riipandi/tango/internal/queue"
)

// Mailer sends transactional email.
type Mailer interface {
	Send(ctx context.Context, msg mailer.Message) error
}

// Job is one recurring maintenance unit.
type Job struct {
	// Name identifies the job and must be unique in the registry.
	Name string

	// Interval is the delay before the next run.
	Interval time.Duration

	// Run performs the work. Errors are retried by the queue.
	Run func(ctx context.Context) error
}

// Registry holds queue registrations and recurring jobs.
type Registry struct {
	queue *queue.Client
	mail  Mailer
	log   logger.Logger

	// jobs is read by maintenance workers.
	jobs map[string]Job
	mu   sync.RWMutex

	// feed supplies /api/version/latest.
	feed *VersionFeed

	// started prevents duplicate initial schedules.
	started sync.Once
}

// NewRegistry registers the email and recurring maintenance queues.
func NewRegistry(client *queue.Client, mail Mailer, log logger.Logger) *Registry {
	r := &Registry{
		queue: client,
		mail:  mail,
		log:   log,
		jobs:  make(map[string]Job),
	}

	client.Register(queue.NewQueue(func(ctx context.Context, task EmailTask) error {
		return r.deliverEmail(ctx, task)
	}))
	client.Register(queue.NewQueue(func(ctx context.Context, task RecurringTask) error {
		return r.runJob(ctx, task)
	}))

	return r
}

// AddJob registers a recurring job and panics on invalid or duplicate names.
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

// Start schedules every recurring job once.
func (r *Registry) Start(ctx context.Context) error {
	r.started.Do(func() {
		for _, job := range r.Jobs() {
			r.schedule(ctx, job, firstDelay(job.Interval))
		}
	})
	return nil
}

// SetVersionFeed attaches the cached release lookup.
func (r *Registry) SetVersionFeed(feed *VersionFeed) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.feed = feed
}

// Latest returns the cached newest release or the deployed build.
func (r *Registry) Latest() string {
	r.mu.RLock()
	feed := r.feed
	r.mu.RUnlock()

	if feed == nil {
		return config.AppVersion
	}
	return feed.Latest()
}

// Stop implements the module lifecycle; queued work drains elsewhere.
func (*Registry) Stop(context.Context) error { return nil }

// Name implements the module contract.
func (*Registry) Name() string { return "jobs" }

// EnqueueEmail adds one transactional email to the queue.
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

// deliverEmail sends one queued message through the shared mailer.
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

// runJob executes a job and schedules its next run even after failure.
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

	// Keep the schedule active when the job fails.
	r.schedule(ctx, job, jobInterval(job, task))
	return runErr
}

// schedule enqueues one delayed job run.
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

// jobInterval uses the queued interval or the registered value.
func jobInterval(job Job, task RecurringTask) time.Duration {
	if carried := task.Interval(); carried > 0 {
		return carried
	}
	return job.Interval
}

// firstDelay spreads initial runs across each job's interval.
func firstDelay(interval time.Duration) time.Duration {
	jitter := jitterFor(interval)
	return jitter + time.Duration(rand.Int64N(int64(interval/2)+1)) //nolint:gosec // scheduling jitter, not a secret
}

// jitterFor adds a small delay to spread runs across instances.
func jitterFor(interval time.Duration) time.Duration {
	jitter := min(MaintenanceJitter, interval/4)
	if jitter <= 0 {
		return 0
	}
	//nolint:gosec // scheduling jitter, not a secret
	return time.Duration(rand.Int64N(int64(jitter) + 1))
}
