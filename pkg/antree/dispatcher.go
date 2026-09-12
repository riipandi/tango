package antree

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type (
	// Dispatcher pulls queued tasks and executes them via queue processors.
	Dispatcher interface {
		// Start starts the dispatcher.
		Start(context.Context)

		// Stop gracefully stops the dispatcher, returning true when all workers
		// finished their tasks.
		Stop(context.Context) bool

		// Notify tells the dispatcher that a new task was added.
		Notify()
	}

	// dispatcher implements Dispatcher.
	dispatcher struct {
		// client is the Client this dispatcher belongs to.
		client *Client

		// log is the logger.
		log Logger

		// ctx is the context used to start the dispatcher.
		ctx context.Context

		// shutdownCtx is an internal context used when attempting a graceful
		// shutdown.
		shutdownCtx context.Context

		// shutdown cancels shutdownCtx.
		shutdown context.CancelFunc

		// numWorkers is the number of goroutines opened to execute tasks.
		numWorkers int

		// releaseAfter is the duration to reclaim a task if it never completed.
		releaseAfter time.Duration

		// cleanupInterval is how often expired completed tasks are removed.
		cleanupInterval time.Duration

		// running indicates if the dispatcher is currently running.
		running atomic.Bool

		// ticker fetches tasks from the database when the next task is delayed.
		ticker *time.Ticker

		// tasks transmits tasks to the workers.
		tasks chan *queuedTask

		// availableWorkers tracks the number of workers available to receive a task.
		availableWorkers chan struct{}

		// ready tells the dispatcher that a database fetch is required.
		ready chan struct{}

		// trigger instructs the dispatcher to fetch tasks from the database now.
		trigger chan struct{}

		// triggered indicates a trigger was sent but not yet received, allowing
		// many ready signals to collapse into a single database fetch.
		triggered atomic.Bool
	}
)

// Start starts the dispatcher. Cancel the provided context for a hard stop;
// call Stop for a graceful one.
func (d *dispatcher) Start(ctx context.Context) {
	// Abort if the dispatcher is already running.
	if d.running.Load() {
		return
	}

	d.ctx = ctx
	d.shutdownCtx, d.shutdown = context.WithCancel(context.Background())
	d.tasks = make(chan *queuedTask, d.numWorkers)
	d.ticker = time.NewTicker(time.Second)
	d.ticker.Stop() // No need to tick yet.
	d.ready = make(chan struct{}, 1000)
	d.trigger = make(chan struct{}, 10)
	d.availableWorkers = make(chan struct{}, d.numWorkers)
	d.running.Store(true)

	for range d.numWorkers {
		go d.worker()
		d.availableWorkers <- struct{}{}
	}

	if d.cleanupInterval > 0 {
		go d.cleaner()
	}

	go d.triggerer()
	go d.fetcher()
	d.ready <- struct{}{}
	d.log.Info("task dispatcher started")
}

// Stop attempts to gracefully shut down the dispatcher, blocking until the
// context is cancelled or all workers are done with their task. True is
// returned when all workers were able to complete.
func (d *dispatcher) Stop(ctx context.Context) bool {
	if !d.running.Load() {
		return true
	}

	// Call the internal shutdown to gracefully close all goroutines.
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

// triggerer listens to the ready channel and forwards a trigger to the fetcher
// only when one is not already pending, collapsing many ready signals into a
// single database fetch.
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
// signal arrives from the triggerer.
func (d *dispatcher) fetcher() {
	defer func() {
		d.running.Store(false)
		d.ticker.Stop()
		close(d.tasks)
		d.log.Info("shutting down dispatcher")
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

// worker processes incoming tasks.
func (d *dispatcher) worker() {
	for {
		select {
		case row := <-d.tasks:
			if row == nil {
				break
			}
			d.processTask(row)
			d.availableWorkers <- struct{}{}
		case <-d.shutdownCtx.Done():
			return
		case <-d.ctx.Done():
			return
		}
	}
}

// cleaner periodically deletes expired completed tasks from the database.
func (d *dispatcher) cleaner() {
	ticker := time.NewTicker(d.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := deleteExpiredCompletedTasks(d.ctx, d.client.db); err != nil {
				d.log.Error("failed to delete expired completed tasks", "error", err)
			}
		case <-d.shutdownCtx.Done():
			return
		case <-d.ctx.Done():
			return
		}
	}
}

// waitForWorkers blocks until at least one worker is available and returns the
// number that are available. Zero is returned when the dispatcher shuts down
// while waiting.
func (d *dispatcher) waitForWorkers() int {
	for {
		select {
		case <-d.shutdownCtx.Done():
			return 0
		case <-d.ctx.Done():
			return 0
		default:
		}

		if w := len(d.availableWorkers); w > 0 {
			return w
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fetch fetches tasks from the database for execution and coordinates when the
// dispatcher needs to fetch again.
func (d *dispatcher) fetch() {
	var err error

	// If we failed at any point, schedule another fetch.
	defer func() {
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			d.ready <- struct{}{}
		}
	}()

	// Incoming task additions from this point on should trigger another fetch.
	d.triggered.Store(false)

	// Fetch only as many tasks as there are workers available.
	workers := d.waitForWorkers()
	if workers == 0 {
		return // shutting down
	}

	// Fetch tasks for each available worker plus the next upcoming task, so the
	// scheduler knows when to query the database again without polling.
	tasks, err := getScheduledTasks(d.ctx, d.client.db, now().Add(-d.releaseAfter), workers+1)
	if err != nil {
		d.log.Error("fetch tasks query failed", "error", err)
		return
	}

	var next *queuedTask
	nextUp := func(i int) {
		next = tasks[i]
		tasks = tasks[:i]
	}

	for i := range tasks {
		// The workers are full.
		if (i + 1) > workers {
			nextUp(i)
			break
		}

		// This task is not ready yet.
		if tasks[i].waitUntil != nil && tasks[i].waitUntil.After(now()) {
			nextUp(i)
			break
		}
	}

	if err = tasks.claim(d.ctx, d.client.db); err != nil {
		d.log.Error("failed to claim tasks", "error", err)
		return
	}

	for i := range tasks {
		tasks[i].attempts++
		<-d.availableWorkers
		d.tasks <- tasks[i]
	}

	d.schedule(next)
}

// schedule adjusts the dispatcher schedule based on the next up task.
func (d *dispatcher) schedule(t *queuedTask) {
	d.ticker.Stop()

	if t == nil {
		return
	}

	if t.waitUntil == nil {
		d.ready <- struct{}{}
		return
	}

	dur := t.waitUntil.Sub(now())
	if dur < 0 {
		d.ready <- struct{}{}
		return
	}
	d.ticker.Reset(dur)
}

// processTask attempts to execute a given task.
func (d *dispatcher) processTask(t *queuedTask) {
	var (
		err    error
		ctx    context.Context
		cancel context.CancelFunc
	)

	q := d.client.queues.get(t.queue)
	cfg := q.Config()

	// Set a context timeout, if desired.
	if cfg.Timeout > 0 {
		ctx, cancel = context.WithDeadline(d.ctx, now().Add(cfg.Timeout))
		defer cancel()
	} else {
		ctx = d.ctx
	}

	// Store the client in the context so the processor can add more tasks.
	ctx = context.WithValue(ctx, ctxKeyClient{}, d.client)

	start := now()
	defer func() {
		// Recover from panics from within the task processor.
		if rec := recover(); rec != nil {
			d.log.Error("panic processing task", "id", t.id, "queue", t.queue, "error", rec)
			err = fmt.Errorf("%v", rec)
		}

		if err != nil {
			d.taskFailure(q, t, start, time.Since(start), err)
		}
	}()

	if err = q.Process(ctx, t.task); err == nil {
		d.taskSuccess(q, t, start, time.Since(start))
	}
}

// taskSuccess removes a successfully executed task from the queue and
// optionally retains it in the completed tasks table.
func (d *dispatcher) taskSuccess(q Queue, t *queuedTask, started time.Time, dur time.Duration) {
	d.log.Info("task processed", "id", t.id, "queue", t.queue, "duration", dur, "attempt", t.attempts)

	var err error
	defer func() {
		if err != nil {
			d.log.Error("failed to update task success", "id", t.id, "queue", t.queue, "error", err)
		}
	}()

	tx, err := d.client.db.Begin(d.ctx)
	if err != nil {
		return
	}
	defer func() {
		if err == nil {
			return
		}
		if rollbackErr := tx.Rollback(d.ctx); rollbackErr != nil {
			d.log.Error("failed to rollback task success", "id", t.id, "queue", t.queue, "error", rollbackErr)
		}
	}()

	if err = t.deleteTx(d.ctx, tx); err != nil {
		return
	}
	if err = d.taskComplete(tx, q, t, started, dur, nil); err != nil {
		return
	}
	err = tx.Commit(d.ctx)
}

// taskFailure releases a failed task back to the queue when attempts remain,
// otherwise deletes it from the queue and optionally moves it to the completed
// tasks table.
func (d *dispatcher) taskFailure(q Queue, t *queuedTask, started time.Time, dur time.Duration, taskErr error) {
	remaining := q.Config().MaxAttempts - t.attempts
	d.log.Error("task processing failed", "id", t.id, "queue", t.queue, "duration", dur, "attempt", t.attempts, "remaining", remaining)

	if remaining >= 1 {
		t.lastExecutedAt = &started
		if err := t.fail(d.ctx, d.client.db, now().Add(q.Config().Backoff)); err != nil {
			d.log.Error("failed to update task failure", "id", t.id, "queue", t.queue, "error", err)
		}
		d.ready <- struct{}{}
		return
	}

	tx, err := d.client.db.Begin(d.ctx)
	if err != nil {
		d.log.Error("failed to update task failure", "id", t.id, "queue", t.queue, "error", err)
		return
	}

	err = t.deleteTx(d.ctx, tx)
	if err == nil {
		err = d.taskComplete(tx, q, t, started, dur, taskErr)
	}
	if err == nil {
		err = tx.Commit(d.ctx)
	}
	if err != nil {
		if rollbackErr := tx.Rollback(d.ctx); rollbackErr != nil {
			d.log.Error("failed to rollback task failure", "id", t.id, "queue", t.queue, "error", rollbackErr)
		}
		d.log.Error("failed to update task failure", "id", t.id, "queue", t.queue, "error", err)
	}
}

// taskComplete creates a completed task from a given task.
func (d *dispatcher) taskComplete(exec Executor, q Queue, t *queuedTask, started time.Time, dur time.Duration, taskErr error) error {
	ret := q.Config().Retention
	if ret == nil {
		return nil
	}
	if taskErr == nil && ret.OnlyFailed {
		return nil
	}

	c := completedTask{
		id:             t.id,
		queue:          t.queue,
		attempts:       t.attempts,
		succeeded:      taskErr == nil,
		lastDuration:   dur,
		createdAt:      t.createdAt,
		lastExecutedAt: started,
	}

	if taskErr != nil {
		errStr := taskErr.Error()
		c.err = &errStr
	}

	if ret.Duration != 0 {
		v := now().Add(ret.Duration)
		c.expiresAt = &v
	}

	if ret.Data != nil && (!ret.Data.OnlyFailed || taskErr != nil) {
		c.task = t.task
	}

	return c.insertTx(d.ctx, exec)
}

// Notify tells the dispatcher that a new task was added.
func (d *dispatcher) Notify() {
	if d.running.Load() {
		d.ready <- struct{}{}
	}
}
