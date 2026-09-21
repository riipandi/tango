package queue

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/riipandi/tango/internal/datastore"
)

// fallbackPoll is the longest the dispatcher stays idle between fetches when
// nothing is scheduled. The engine is event-driven — a save, a notify, or a
// scheduled task wakes it — but a task whose worker was lost is only
// reclaimable after ReleaseAfter, and nothing else fires then, so the
// fallback keeps the reclaim bounded. One cheap indexed query a minute is
// what a quiet queue costs.
const fallbackPoll = time.Minute

// requeueDelay is how long a task whose queue was never registered waits
// before the dispatcher looks at it again. It cannot be processed and cannot
// be dropped — the queue may be registered by a deploy on its way — so it is
// retried on this clock, and no attempt beyond the claim is spent on it.
const requeueDelay = time.Minute

// retryDelay is how long a failed fetch waits before it tries again: a
// database that just refused a claim is given a second, not another one
// right away.
const retryDelay = time.Second

// dispatcher claims queued tasks and hands them to a pool of workers.
//
// One start runs one generation of goroutines: a triggerer that folds any
// number of ready signals into a single fetch, a fetcher that claims tasks
// from the database and schedules the next fetch, and the workers that
// execute them. The completed-table cleanup is not here: it is a job of its
// own (internal/jobs), because recurring maintenance belongs there, not in
// the engine.
type dispatcher struct {
	// client is the client this dispatcher belongs to.
	client *Client
	// ctx is the context the dispatcher was started with, and the parent of
	// every task execution.
	ctx context.Context
	// shutdownCtx cancels when a graceful stop is asked for. It is separate
	// from ctx so a stop is observable while the start context is still live.
	shutdownCtx      context.Context
	shutdown         context.CancelFunc
	numWorkers       int
	releaseAfter     time.Duration
	running          atomic.Bool
	ticker           *time.Ticker
	tasks            chan *taskRow
	availableWorkers chan struct{}
	// ready reports that fetching is worth considering: a task was added.
	ready chan struct{}
	// trigger carries one fetch instruction from the triggerer to the
	// fetcher. The triggered flag folds a burst of ready signals into the
	// one fetch that covers them all, so a bulk insert fetches once.
	trigger   chan struct{}
	triggered atomic.Bool
}

// init wires the dispatcher to its client. Called once, before start.
func (d *dispatcher) init(client *Client, numWorkers int, releaseAfter time.Duration) {
	d.client = client
	d.numWorkers = numWorkers
	d.releaseAfter = releaseAfter
}

// start starts the dispatcher. To hard-stop it, cancel the provided context;
// to stop it gracefully, call stop.
func (d *dispatcher) start(ctx context.Context) {
	// Abort if the dispatcher is already running.
	if d.running.Load() {
		return
	}

	d.ctx = ctx
	d.shutdownCtx, d.shutdown = context.WithCancel(context.Background())
	d.tasks = make(chan *taskRow, d.numWorkers)
	d.ticker = time.NewTicker(fallbackPoll)
	d.ticker.Stop() // No need to poll yet.
	d.ready = make(chan struct{}, 1000)
	d.trigger = make(chan struct{}, 10)
	d.availableWorkers = make(chan struct{}, d.numWorkers)
	d.running.Store(true)

	for range d.numWorkers {
		go d.worker()
		d.availableWorkers <- struct{}{}
	}
	go d.triggerer()
	go d.fetcher()
	d.ready <- struct{}{}
	d.client.log.InfoContext(ctx, "task dispatcher started", "workers", d.numWorkers)
}

// stop attempts to gracefully shut down the dispatcher by blocking until
// either the context is cancelled or every worker is done with its task.
func (d *dispatcher) stop(ctx context.Context) bool {
	if !d.running.Load() {
		return true
	}

	d.shutdown()
	var count int
	for {
		select {
		case <-ctx.Done():
			return false
		case <-d.availableWorkers:
			count++
			if count == d.numWorkers {
				return true
			}
		}
	}
}

// triggerer listens to the ready channel and sends a trigger to the fetcher
// only when one is not already on its way, so a hundred tasks added in a
// loop cost one fetch, not a hundred.
func (d *dispatcher) triggerer() {
	for {
		select {
		case <-d.ready:
			if d.triggered.CompareAndSwap(false, true) {
				d.trigger <- struct{}{}
			}
		case <-d.shutdownCtx.Done():
			return
		case <-d.ctx.Done():
			return
		}
	}
}

// fetcher fetches tasks from the database when the ticker ticks or a trigger
// arrives, and stops when either context does.
func (d *dispatcher) fetcher() {
	defer func() {
		d.running.Store(false)
		d.ticker.Stop()
		close(d.tasks)
		d.client.log.InfoContext(d.ctx, "shutting down dispatcher")
	}()

	for {
		select {
		case <-d.ticker.C:
			d.ticker.Stop()
			d.fetch()
		case <-d.trigger:
			d.fetch()
		case <-d.shutdownCtx.Done():
			return
		case <-d.ctx.Done():
			return
		}
	}
}

// worker processes incoming tasks until the dispatcher stops.
func (d *dispatcher) worker() {
	for {
		select {
		case task := <-d.tasks:
			if task == nil {
				break
			}
			d.processTask(task)
			d.availableWorkers <- struct{}{}
		case <-d.shutdownCtx.Done():
			return
		case <-d.ctx.Done():
			return
		}
	}
}

// fetch claims the tasks the workers are free to run and schedules the next
// fetch. A failure schedules too — the fetcher retries on the fallback clock
// rather than hammering a struggling database.
func (d *dispatcher) fetch() {
	// Indicate that a task added from this point on should trigger another
	// fetch: the state this run reads is about to be settled.
	d.triggered.Store(false)

	// Every worker free to run is a task worth claiming, so the pool is never
	// handed more than it can take.
	var workers int
	for workers = len(d.availableWorkers); workers == 0; workers = len(d.availableWorkers) {
		time.Sleep(100 * time.Millisecond)
	}

	at := now()
	deadline := at.Add(-d.releaseAfter)
	tasks, err := claimReady(d.ctx, d.client.store, at, deadline, workers)
	if err != nil {
		d.client.log.ErrorContext(d.ctx, "queue: failed to claim tasks", "error", err)
		d.scheduleRetry()
		return
	}

	// One token goes with each claimed task, so the pool's free count is the
	// tokens left: a worker returning from its task puts its own back.
	for _, task := range tasks {
		<-d.availableWorkers
		d.tasks <- task
	}

	// The next fetch waits for the next claimable task, whichever queue it
	// belongs to.
	exists, wait, err := peekNext(d.ctx, d.client.store, deadline)
	if err != nil {
		d.client.log.ErrorContext(d.ctx, "queue: failed to peek next task", "error", err)
	}
	d.schedule(exists, wait)
}

// scheduleRetry arms the fallback clock after a failed fetch: the database
// that just refused a claim is given a second, not another one right away.
func (d *dispatcher) scheduleRetry() {
	d.ticker.Stop()
	d.ticker.Reset(retryDelay)
}

// schedule arms the ticker for the next fetch. No next task arms only the
// fallback, so a quiet queue wakes on its own clock — that fallback is what
// reclaims a task whose worker was lost. A ready-but-unclaimed task fetches
// again right away, and a delayed one waits for it, capped by the fallback
// so a lost claim is still reclaimed on time.
func (d *dispatcher) schedule(exists bool, wait *time.Time) {
	d.ticker.Stop()

	if !exists {
		d.ticker.Reset(fallbackPoll)
		return
	}
	if wait == nil {
		d.ready <- struct{}{}
		return
	}

	delay := time.Until(*wait)
	if delay <= 0 {
		d.ready <- struct{}{}
		return
	}
	if delay > fallbackPoll {
		delay = fallbackPoll
	}
	d.ticker.Reset(delay)
}

// notify tells the dispatcher that new tasks were added. A notification
// dropped because the buffer is full is fine: the fetch the earlier signals
// armed covers the tasks that arrived since.
func (d *dispatcher) notify() {
	if d.running.Load() {
		d.ready <- struct{}{}
	}
}

// processTask executes one claimed task and settles its outcome: success
// deletes it, a retryable failure releases it for its backoff, and a final
// one archives it. The processor's panic is a failure, not a crash.
func (d *dispatcher) processTask(task *taskRow) {
	queue, ok := d.client.queues.get(task.Queue)
	if !ok {
		// The queue is unknown to this process. The task keeps its place and
		// waits for the registration that will process it; the claim's
		// attempt is the only one it costs.
		d.client.log.WarnContext(d.ctx, "queue: task for unregistered queue",
			"id", task.ID, "queue", task.Queue)
		if err := requeueTask(d.ctx, d.client.store, task.ID, now().Add(requeueDelay)); err != nil {
			d.client.log.ErrorContext(d.ctx, "queue: failed to requeue task", "error", err)
		}
		return
	}

	cfg := queue.Config()

	// The execution context is the dispatcher's, deadlined by the queue's
	// timeout when it sets one, and it carries the client so a task can
	// enqueue the task that follows it.
	ctx := d.ctx
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(d.ctx, now().Add(cfg.Timeout))
		defer cancel()
	}
	ctx = context.WithValue(ctx, ctxKeyClient{}, d.client)

	start := now()
	err := d.runProcessor(ctx, queue, task)
	duration := time.Since(start)

	if err == nil {
		d.taskSuccess(ctx, queue, task, start, duration)
		return
	}
	d.taskFailure(ctx, queue, task, start, duration, err)
}

// runProcessor invokes the queue's callback, turning a panic into the error
// the outcome settles on.
func (d *dispatcher) runProcessor(ctx context.Context, queue Queue, task *taskRow) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			d.client.log.ErrorContext(ctx, "queue: panic processing task",
				"id", task.ID, "queue", task.Queue, "panic", rec)
			err = fmt.Errorf("%v", rec)
		}
	}()
	return queue.Process(ctx, task.Payload)
}

// taskSuccess settles a successful execution: the task leaves the pending
// table and, when the queue's retention asks for it, arrives in the
// completed one. One transaction covers both, so a task is never in neither.
func (d *dispatcher) taskSuccess(ctx context.Context, queue Queue, task *taskRow, started time.Time, duration time.Duration) {
	d.client.log.InfoContext(ctx, "task processed",
		"id", task.ID, "queue", task.Queue, "duration", duration, "attempt", task.Attempts)

	retention := queue.Config().Retention
	if retention == nil || retention.OnlyFailed {
		if err := deleteTask(ctx, d.client.store, task.ID); err != nil {
			d.client.log.ErrorContext(ctx, "queue: failed to delete task", "error", err)
		}
		return
	}

	completed := d.completedRecord(queue, task, started, duration, nil)
	d.archive(ctx, task, completed)
}

// taskFailure settles a failed execution: a task with attempts left is
// released for its backoff, and a task that exhausted them is archived with
// the error that finished it.
func (d *dispatcher) taskFailure(ctx context.Context, queue Queue, task *taskRow, started time.Time, duration time.Duration, taskErr error) {
	remaining := queue.Config().MaxAttempts - task.Attempts
	d.client.log.ErrorContext(ctx, "task processing failed",
		"id", task.ID, "queue", task.Queue, "duration", duration,
		"attempt", task.Attempts, "remaining", remaining, "error", taskErr)

	if remaining >= 1 {
		if err := requeueTask(d.ctx, d.client.store, task.ID, now().Add(queue.Config().Backoff)); err != nil {
			d.client.log.ErrorContext(ctx, "queue: failed to requeue task", "error", err)
		}
		d.ready <- struct{}{}
		return
	}

	retention := queue.Config().Retention
	if retention == nil {
		if err := deleteTask(ctx, d.client.store, task.ID); err != nil {
			d.client.log.ErrorContext(ctx, "queue: failed to delete task", "error", err)
		}
		return
	}

	completed := d.completedRecord(queue, task, started, duration, taskErr)
	d.archive(ctx, task, completed)
}

// archive moves a finished task out of the pending table and into the
// completed one in one transaction.
func (d *dispatcher) archive(ctx context.Context, task *taskRow, completed *completedRow) {
	err := d.client.store.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		if err := deleteTask(ctx, tx, task.ID); err != nil {
			return err
		}
		return insertCompleted(ctx, tx, completed)
	})
	if err != nil {
		d.client.log.ErrorContext(ctx, "queue: failed to archive task",
			"id", task.ID, "queue", task.Queue, "error", err)
	}
}

// completedRecord builds the archived form of a finished task under the
// queue's retention policy.
func (d *dispatcher) completedRecord(queue Queue, task *taskRow, started time.Time, duration time.Duration, taskErr error) *completedRow {
	retention := queue.Config().Retention
	c := &completedRow{
		ID:             task.ID,
		Queue:          task.Queue,
		Attempts:       task.Attempts,
		Succeeded:      taskErr == nil,
		LastDuration:   duration,
		CreatedAt:      task.CreatedAt,
		LastExecutedAt: started,
	}
	if taskErr != nil {
		message := taskErr.Error()
		c.Error = &message
	}
	if retention.Duration > 0 {
		expires := now().Add(retention.Duration)
		c.ExpiresAt = &expires
	}
	if retention.Data != nil && (!retention.Data.OnlyFailed || taskErr != nil) {
		c.Payload = task.Payload
	}
	return c
}
