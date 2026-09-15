package queue

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/riipandi/tango/internal/datastore"
)

type (
	// Dispatcher pulls queued tasks and runs their processors.
	Dispatcher interface {
		Start(context.Context)

		// Stop stops gracefully and reports whether workers finished.
		Stop(context.Context) bool

		// Notify signals that a new task was added.
		Notify()
	}

	// dispatcher implements Dispatcher.
	dispatcher struct {
		client          *Client
		log             Logger
		numWorkers      int
		releaseAfter    time.Duration
		cleanupInterval time.Duration

		running atomic.Bool

		// wg tracks active goroutines.
		wg sync.WaitGroup

		ctx context.Context

		// shutdownCtx controls graceful shutdown.
		shutdownCtx context.Context
		shutdown    context.CancelFunc

		// ticker wakes the fetcher for delayed tasks.
		ticker *time.Ticker

		// tasks sends tasks to workers.
		tasks chan *queuedTask

		// availableWorkers tracks idle workers.
		availableWorkers chan struct{}

		// ready signals that a database fetch is needed.
		ready chan struct{}

		// trigger asks the fetcher to query now.
		trigger chan struct{}

		// triggered coalesces pending fetch signals.
		triggered atomic.Bool
	}
)

// Start starts the dispatcher.
func (d *dispatcher) Start(ctx context.Context) {
	if d.running.Load() {
		return
	}
	d.wg.Wait()

	d.ctx = ctx
	d.shutdownCtx, d.shutdown = context.WithCancel(context.Background())
	d.tasks = make(chan *queuedTask, d.numWorkers)
	d.ticker = time.NewTicker(time.Second)
	d.ticker.Stop()
	d.ready = make(chan struct{}, 1000)
	d.trigger = make(chan struct{}, 10)
	d.availableWorkers = make(chan struct{}, d.numWorkers)
	d.running.Store(true)

	// Goroutines use these local channel references.
	tasks, ready, trigger, available := d.tasks, d.ready, d.trigger, d.availableWorkers
	ticker, shutdownCtx := d.ticker, d.shutdownCtx

	d.wg.Add(1)
	go d.triggerer(ctx, shutdownCtx, ready, trigger)

	d.wg.Add(1)
	go d.fetcher(ctx, shutdownCtx, tasks, ticker, ready, trigger)

	if d.cleanupInterval > 0 {
		d.wg.Add(1)
		go d.cleaner(ctx, shutdownCtx)
	}

	for range d.numWorkers {
		d.wg.Add(1)
		go d.worker(ctx, shutdownCtx, tasks, available, ready)
		available <- struct{}{}
	}

	ready <- struct{}{}
	d.log.Info("task dispatcher started")
}

// Stop shuts down gracefully and reports whether workers finished in time.
func (d *dispatcher) Stop(ctx context.Context) bool {
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

// triggerer coalesces ready signals into fetch triggers.
func (d *dispatcher) triggerer(ctx, shutdownCtx context.Context, ready, trigger chan struct{}) {
	defer d.wg.Done()

	for {
		select {
		case <-ready:
			if d.triggered.CompareAndSwap(false, true) {
				trigger <- struct{}{}
			}
		case <-shutdownCtx.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

// fetcher loads tasks when the ticker or trigger fires.
func (d *dispatcher) fetcher(ctx, shutdownCtx context.Context, tasks chan *queuedTask, ticker *time.Ticker, ready, trigger chan struct{}) {
	defer d.wg.Done()
	defer func() {
		d.running.Store(false)
		ticker.Stop()
		close(tasks)
		d.log.Info("shutting down dispatcher")
	}()

	for {
		select {
		case <-ticker.C:
			ticker.Stop()
			d.fetch(ctx, tasks, ticker, ready, trigger)
		case <-trigger:
			d.fetch(ctx, tasks, ticker, ready, trigger)
		case <-shutdownCtx.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

// worker processes incoming tasks.
func (d *dispatcher) worker(ctx, shutdownCtx context.Context, tasks chan *queuedTask, available, ready chan struct{}) {
	defer d.wg.Done()

	for {
		select {
		case row := <-tasks:
			if row == nil {
				break
			}
			d.processTask(ctx, ready, row)
			available <- struct{}{}
		case <-shutdownCtx.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

// cleaner deletes expired completed tasks.
func (d *dispatcher) cleaner(ctx, shutdownCtx context.Context) {
	defer d.wg.Done()

	ticker := time.NewTicker(d.cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := deleteExpiredCompletedTasks(ctx, d.client.store); err != nil {
				d.log.Error("failed to delete expired completed tasks", "error", err)
			}
		case <-shutdownCtx.Done():
			return
		case <-ctx.Done():
			return
		}
	}
}

// waitForWorkers returns the number of idle workers, or zero on shutdown.
func (d *dispatcher) waitForWorkers(ctx context.Context, available chan struct{}) int {
	for {
		select {
		case <-d.shutdownCtx.Done():
			return 0
		case <-ctx.Done():
			return 0
		default:
		}

		if w := len(available); w > 0 {
			return w
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// fetch loads due tasks and hands them to workers.
func (d *dispatcher) fetch(ctx context.Context, tasks chan *queuedTask, ticker *time.Ticker, ready, trigger chan struct{}) {
	var err error

	// Retry after a fetch failure.
	defer func() {
		if err != nil {
			time.Sleep(100 * time.Millisecond)
			ready <- struct{}{}
		}
	}()

	// New task signals belong to the next fetch.
	d.triggered.Store(false)

	// Fetch no more tasks than idle workers can handle.
	workers := d.waitForWorkers(ctx, d.availableWorkers)
	if workers == 0 {
		return
	}

	// Load one extra row to schedule the next fetch without polling.
	queued, err := getScheduledTasks(ctx, d.client.store, now().Add(-d.releaseAfter), workers+1)
	if err != nil {
		d.log.Error("fetch tasks query failed", "error", err)
		return
	}

	var next *queuedTask
	nextUp := func(i int) {
		next = queued[i]
		queued = queued[:i]
	}

	for i := range queued {
		// All workers are full.
		if (i + 1) > workers {
			nextUp(i)
			break
		}

		// The task is scheduled for later.
		if queued[i].waitUntil != nil && queued[i].waitUntil.After(now()) {
			nextUp(i)
			break
		}
	}

	// Keep tasks claimed by another dispatcher with the winner.
	claimed, claimErr := queued.claim(ctx, d.client.store, now().Add(-d.releaseAfter))
	if claimErr != nil {
		err = claimErr
		d.log.Error("failed to claim tasks", "error", claimErr)
		return
	}
	won := make(map[string]struct{}, len(claimed))
	for _, id := range claimed {
		won[id] = struct{}{}
	}

	for i := range queued {
		if _, ok := won[queued[i].id]; !ok {
			continue
		}
		queued[i].attempts++
		<-d.availableWorkers
		tasks <- queued[i]
	}

	d.schedule(ticker, ready, next)
}

// schedule sets the next fetch time.
func (d *dispatcher) schedule(ticker *time.Ticker, ready chan struct{}, t *queuedTask) {
	ticker.Stop()

	if t == nil {
		return
	}

	if t.waitUntil == nil {
		ready <- struct{}{}
		return
	}

	dur := t.waitUntil.Sub(now())
	if dur < 0 {
		ready <- struct{}{}
		return
	}
	ticker.Reset(dur)
}

// processTask executes one task.
func (d *dispatcher) processTask(ctx context.Context, ready chan struct{}, t *queuedTask) {
	var err error

	q, ok := d.client.queues.lookup(t.queue)
	if !ok {
		// An unknown queue cannot run; discard its task.
		d.log.Error("queue not registered, discarding task", "id", t.id, "queue", t.queue)
		if delErr := t.deleteTx(ctx, d.client.store); delErr != nil {
			d.log.Error("failed to discard task", "id", t.id, "queue", t.queue, "error", delErr)
		}
		return
	}
	cfg := q.Config()

	// Measure the execution timeout with wall time.
	if cfg.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, cfg.Timeout)
		defer cancel()
	}

	// Processors use the context to add more tasks.
	ctx = context.WithValue(ctx, ctxKeyClient{}, d.client)

	start := now()
	defer func() {
		if rec := recover(); rec != nil {
			d.log.Error("panic processing task", "id", t.id, "queue", t.queue, "error", rec)
			err = fmt.Errorf("%v", rec)
		}

		if err != nil {
			d.taskFailure(ctx, ready, q, t, start, time.Since(start), err)
		}
	}()

	if err = q.Process(ctx, t.task); err == nil {
		d.taskSuccess(ctx, q, t, start, time.Since(start))
	}
}

// taskSuccess removes a successful task and records it when configured.
func (d *dispatcher) taskSuccess(ctx context.Context, q Queue, t *queuedTask, started time.Time, dur time.Duration) {
	d.log.Info("task processed", "id", t.id, "queue", t.queue, "duration", dur, "attempt", t.attempts)

	err := d.client.store.WithTx(ctx, func(exec datastore.Executor) error {
		if err := t.deleteTx(ctx, exec); err != nil {
			return err
		}
		return d.taskComplete(ctx, exec, q, t, started, dur, nil)
	})
	if err != nil {
		d.log.Error("failed to update task success", "id", t.id, "queue", t.queue, "error", err)
	}
}

// taskFailure retries a failed task or records its final failure.
func (d *dispatcher) taskFailure(ctx context.Context, ready chan struct{}, q Queue, t *queuedTask, started time.Time, dur time.Duration, taskErr error) {
	remaining := q.Config().MaxAttempts - t.attempts
	d.log.Error("task processing failed", "id", t.id, "queue", t.queue, "duration", dur, "attempt", t.attempts, "remaining", remaining)

	if remaining >= 1 {
		t.lastExecutedAt = &started
		if err := t.fail(ctx, d.client.store, now().Add(q.Config().Backoff)); err != nil {
			d.log.Error("failed to update task failure", "id", t.id, "queue", t.queue, "error", err)
		}
		// Fetch again using the new wait time.
		ready <- struct{}{}
		return
	}

	err := d.client.store.WithTx(ctx, func(exec datastore.Executor) error {
		if err := t.deleteTx(ctx, exec); err != nil {
			return err
		}
		return d.taskComplete(ctx, exec, q, t, started, dur, taskErr)
	})
	if err != nil {
		d.log.Error("failed to update task failure", "id", t.id, "queue", t.queue, "error", err)
	}
}

// taskComplete records a task when retention is enabled.
func (d *dispatcher) taskComplete(ctx context.Context, exec datastore.Executor, q Queue, t *queuedTask, started time.Time, dur time.Duration, taskErr error) error {
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

	return c.insertTx(ctx, exec)
}

// Notify signals that a new task was added.
func (d *dispatcher) Notify() {
	if d.running.Load() {
		d.ready <- struct{}{}
	}
}
