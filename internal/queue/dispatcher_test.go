package queue

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDispatcherNotify(t *testing.T) {
	d := dispatcher{ready: make(chan struct{}, 1)}

	d.Notify()
	select {
	case <-d.ready:
		t.Error("ready message was sent")
	default:
	}

	d.running.Store(true)
	d.Notify()
	select {
	case <-d.ready:
	default:
		t.Error("ready message was not sent")
	}
}

func TestDispatcherStart(t *testing.T) {
	d := newDispatcher(t)

	// Starting an active dispatcher is a no-op.
	d.running.Store(true)
	d.Start(context.Background())
	assert.Nil(t, d.ctx, "ctx")
	assert.Equal(t, 0, cap(d.tasks), "tasks channel")
	assert.Equal(t, 0, cap(d.ready), "ready channel")
	assert.Equal(t, 0, cap(d.trigger), "trigger channel")
	assert.Equal(t, 0, cap(d.availableWorkers), "available workers channel")
	assert.True(t, d.running.Load(), "running")

	// Start when stopped.
	d.running.Store(false)
	ctx, cancel := context.WithCancel(context.Background())
	d.Start(ctx)
	assert.Equal(t, ctx, d.ctx, "ctx")
	assert.Equal(t, d.numWorkers, cap(d.tasks), "tasks channel")
	assert.Equal(t, 1000, cap(d.ready), "ready channel")
	assert.Equal(t, 10, cap(d.trigger), "trigger channel")
	assert.Equal(t, d.numWorkers, cap(d.availableWorkers), "available workers channel")
	assert.Equal(t, d.numWorkers, len(d.availableWorkers), "available workers channel length")
	assert.True(t, d.running.Load(), "running")

	// Context cancellation shuts down the dispatcher.
	cancel()
	wait()
	assert.False(t, d.running.Load(), "running")
}

func TestDispatcherStop(t *testing.T) {
	d := newDispatcher(t)
	ctx := context.Background()

	// The dispatcher is stopped.
	assert.True(t, d.Stop(ctx))
	assert.False(t, d.running.Load(), "running")

	// All workers are idle.
	d.Start(ctx)
	assert.True(t, d.Stop(ctx))
	wait()
	wait()
	assert.False(t, d.running.Load(), "running")
	select {
	case <-d.shutdownCtx.Done():
	default:
		t.Error("shutdown context was not cancelled")
	}

	// One worker is busy.
	d.Start(ctx)
	<-d.availableWorkers
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Millisecond)
	defer cancel()
	assert.False(t, d.Stop(stopCtx))
	wait()
	wait()
	assert.False(t, d.running.Load(), "running")
}

func TestDispatcherTriggerer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	d := &dispatcher{
		ready:       make(chan struct{}, 5),
		trigger:     make(chan struct{}, 5),
		shutdownCtx: context.Background(),
		ctx:         ctx,
	}
	d.wg.Add(1)
	go d.triggerer(ctx, d.shutdownCtx, d.ready, d.trigger)

	// One ready signal produces one trigger.
	d.ready <- struct{}{}
	waitForChan(t, d.trigger)
	d.triggered.Store(false)

	// Multiple ready signals coalesce into one trigger.
	d.ready <- struct{}{}
	d.ready <- struct{}{}
	d.ready <- struct{}{}
	wait()
	require.Len(t, d.trigger, 1, "trigger")
	<-d.trigger
	d.triggered.Store(false)

	// A canceled main context produces no trigger.
	cancel()
	d.ready <- struct{}{}
	wait()
	assert.Len(t, d.trigger, 0, "trigger")

	// A canceled shutdown context produces no trigger.
	ctx, cancel = context.WithCancel(context.Background())
	d = &dispatcher{
		ready:       make(chan struct{}, 5),
		trigger:     make(chan struct{}, 5),
		shutdownCtx: ctx,
		ctx:         context.Background(),
	}
	d.wg.Add(1)
	go d.triggerer(ctx, d.shutdownCtx, d.ready, d.trigger)
	cancel()
	wait()
	d.ready <- struct{}{}
	wait()
	assert.Len(t, d.trigger, 0, "trigger")
}

func TestDispatcherCleaner(t *testing.T) {
	store := newPool(t)
	d := &dispatcher{
		numWorkers: 1,
		client:     &Client{store: poolStore{pool: store}},
		log:        &noLogger{},
	}
	task := completedTask{
		queue:          "test",
		attempts:       1,
		succeeded:      false,
		createdAt:      time.Now(),
		lastExecutedAt: time.Now(),
	}

	// Cleanup disabled: expired tasks remain.
	d.Start(context.Background())
	task.id = nextTaskID()
	task.expiresAt = pointer(time.Now())
	insertCompleted(t, store, task)
	wait()
	require.Len(t, getCompletedTasks(t, store), 1)
	d.Stop(context.Background())
	wait()

	// Cleanup enabled: expired tasks are deleted.
	d.cleanupInterval = 2 * time.Millisecond
	d.Start(context.Background())
	wait()
	require.Len(t, getCompletedTasks(t, store), 0)
	d.Stop(context.Background())
	wait()

	// Check different expiration conditions.
	task.id = nextTaskID()
	task.expiresAt = nil
	insertCompleted(t, store, task)
	keptID := nextTaskID()
	task.id = keptID
	task.expiresAt = pointer(time.Now().Add(time.Hour))
	insertCompleted(t, store, task)
	task.id = nextTaskID()
	task.expiresAt = pointer(time.Now().Add(time.Millisecond))
	insertCompleted(t, store, task)
	lateID := nextTaskID()
	task.id = lateID
	task.expiresAt = pointer(time.Now().Add(150 * time.Millisecond))
	insertCompleted(t, store, task)

	d.Start(context.Background())
	wait()
	completedTaskIDsExist(t, store, []string{keptID, lateID})
	wait()
	completedTaskIDsExist(t, store, []string{keptID})
	d.Stop(context.Background())
}

func TestDispatcherProcessTaskContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var innerCtx context.Context

	d := newDispatcher(t)
	d.ctx = ctx

	var called bool
	d.client.Register(NewQueue(func(ctx context.Context, _ testTask) error {
		called = true
		innerCtx = ctx

		// The deadline starts here; a full second remains.
		deadline, ok := ctx.Deadline()
		require.True(t, ok, "deadline set")
		assert.Equal(t, d.client, FromContext(ctx), "client")
		remaining := time.Until(deadline)
		assert.LessOrEqual(t, remaining, time.Second, "ctx deadline")
		assert.Greater(t, remaining, 900*time.Millisecond, "ctx deadline")
		return nil
	}))

	d.processTask(d.ctx, d.ready, &queuedTask{
		id:        nextTaskID(),
		queue:     "test",
		task:      encode(t, &testTask{Val: "1"}),
		attempts:  1,
		createdAt: time.Now(),
	})
	assert.True(t, called, "called")

	cancel()
	select {
	case <-innerCtx.Done():
	default:
		t.Error("cancel did not cancel inner context")
	}
}

func TestDispatcherProcessTaskSuccess(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	var called bool
	d.client.Register(NewQueue(func(_ context.Context, task testTask) error {
		called = true
		assert.Equal(t, "1", task.Val, "task val")
		return nil
	}))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test",
		task:      encode(t, &testTask{Val: "1"}),
		attempts:  1,
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	d.processTask(d.ctx, d.ready, tk)
	assert.True(t, called, "called")
	require.Len(t, getTasks(t, d.client.store), 0)
	assert.Len(t, d.ready, 0, "ready")

	completed := getCompletedTasks(t, d.client.store)
	require.Len(t, completed, 1)
	assert.Equal(t, tk.id, completed[0].id, "id")
	assert.Equal(t, "test", completed[0].queue, "queue")
	assert.Equal(t, 1, completed[0].attempts, "attempts")
	assert.True(t, completed[0].succeeded, "succeeded")
	assert.Equal(t, now(), completed[0].createdAt, "created at")
	assert.Equal(t, now(), completed[0].lastExecutedAt, "last executed at")
	assert.Equal(t, now().Add(time.Hour).UnixMilli(), completed[0].expiresAt.UnixMilli(), "expires at")
	assert.Nil(t, completed[0].err, "error")
	assert.True(t, bytes.Equal(tk.task, completed[0].task), "task does not match")
	assert.Greater(t, completed[0].lastDuration, time.Duration(0), "last duration not set")
}

func TestDispatcherProcessTaskNoRetention(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	d.client.Register(NewQueue(func(_ context.Context, _ testTaskNoRetention) error { return nil }))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test-noret",
		task:      encode(t, &testTaskNoRetention{Val: "1"}),
		attempts:  1,
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	d.processTask(d.ctx, d.ready, tk)
	require.Len(t, getTasks(t, d.client.store), 0)
	require.Len(t, getCompletedTasks(t, d.client.store), 0)
}

func TestDispatcherProcessTaskRetainNoData(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	d.client.Register(NewQueue(func(_ context.Context, _ testTaskRetainNoData) error { return nil }))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test-retainnodata",
		task:      encode(t, &testTaskRetainNoData{Val: "1"}),
		attempts:  1,
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	d.processTask(d.ctx, d.ready, tk)
	require.Len(t, getTasks(t, d.client.store), 0)

	completed := getCompletedTasks(t, d.client.store)
	require.Len(t, completed, 1)
	assert.Nil(t, completed[0].task, "task data shouldn't have been retained")
}

func TestDispatcherProcessTaskRetainForever(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	d.client.Register(NewQueue(func(_ context.Context, _ testTaskRetainForever) error { return nil }))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test-retainforever",
		task:      encode(t, &testTaskRetainForever{Val: "1"}),
		attempts:  1,
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	d.processTask(d.ctx, d.ready, tk)
	require.Len(t, getTasks(t, d.client.store), 0)

	completed := getCompletedTasks(t, d.client.store)
	require.Len(t, completed, 1)
	assert.Nil(t, completed[0].expiresAt, "expires at")
}

func TestDispatcherProcessTaskRetainDataFailed(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	var succeed bool
	d.client.Register(NewQueue(func(_ context.Context, _ testTaskRetainDataFailed) error {
		if succeed {
			return nil
		}
		return errors.New("fail")
	}))

	for range 2 {
		succeed = !succeed
		tk := &queuedTask{
			id:        nextTaskID(),
			queue:     "test-retaindatafailed",
			task:      encode(t, &testTaskRetainDataFailed{Val: "1"}),
			attempts:  2,
			createdAt: now(),
		}
		insertTask(t, d.client.store, tk)

		d.processTask(d.ctx, d.ready, tk)
		require.Len(t, getTasks(t, d.client.store), 0)

		completed := getCompletedTasks(t, d.client.store)
		require.Len(t, completed, 1)
		if succeed {
			assert.Nil(t, completed[0].task, "task data shouldn't have been retained")
		} else {
			assert.NotNil(t, completed[0].task, "task data should have been retained")
		}
		deleteCompletedTasks(t, d.client.store)
	}
}

func TestDispatcherProcessTaskRetainFailed(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	var succeed bool
	d.client.Register(NewQueue(func(_ context.Context, _ testTaskRetainFailed) error {
		if succeed {
			return nil
		}
		return errors.New("fail")
	}))

	for range 2 {
		succeed = !succeed
		tk := &queuedTask{
			id:        nextTaskID(),
			queue:     "test-retainfailed",
			task:      encode(t, &testTaskRetainFailed{Val: "1"}),
			attempts:  2,
			createdAt: now(),
		}
		insertTask(t, d.client.store, tk)

		d.processTask(d.ctx, d.ready, tk)
		require.Len(t, getTasks(t, d.client.store), 0)

		completed := getCompletedTasks(t, d.client.store)
		if succeed {
			assert.Len(t, completed, 0)
		} else {
			assert.Len(t, completed, 1)
		}
	}
}

func TestDispatcherProcessTaskPanic(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	var called bool
	d.client.Register(NewQueue(func(_ context.Context, _ testTask) error {
		called = true
		panic("panic called")
	}))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test",
		task:      encode(t, &testTask{Val: "1"}),
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	d.processTask(d.ctx, d.ready, tk)
	assert.True(t, called, "called")
	waitForChan(t, d.ready)

	got := getTasks(t, d.client.store)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].lastExecutedAt)
	assert.Equal(t, now().UnixMilli(), got[0].lastExecutedAt.UnixMilli(), "last executed at")
	require.NotNil(t, got[0].waitUntil)
	assert.Equal(t, now().Add(5*time.Millisecond).UnixMilli(), got[0].waitUntil.UnixMilli(), "wait until")
}

func TestDispatcherProcessTaskFailure(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	var called bool
	d.client.Register(NewQueue(func(_ context.Context, _ testTask) error {
		called = true
		return errors.New("failure error")
	}))

	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "test",
		task:      encode(t, &testTask{Val: "1"}),
		attempts:  1,
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	// First attempt is released back to the queue.
	d.processTask(d.ctx, d.ready, tk)
	assert.True(t, called, "called")
	waitForChan(t, d.ready)

	got := getTasks(t, d.client.store)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].lastExecutedAt)
	assert.Equal(t, now().UnixMilli(), got[0].lastExecutedAt.UnixMilli(), "last executed at")
	require.NotNil(t, got[0].waitUntil)
	assert.Equal(t, now().Add(5*time.Millisecond).UnixMilli(), got[0].waitUntil.UnixMilli(), "wait until")

	// Final attempt moves to completed with the error.
	called = false
	tk.attempts++
	d.processTask(d.ctx, d.ready, tk)
	assert.True(t, called, "called")
	assert.Len(t, d.ready, 0, "ready")
	require.Len(t, getTasks(t, d.client.store), 0)

	completed := getCompletedTasks(t, d.client.store)
	require.Len(t, completed, 1)
	assert.Equal(t, tk.id, completed[0].id, "id")
	assert.Equal(t, "test", completed[0].queue, "queue")
	assert.Equal(t, 2, completed[0].attempts, "attempts")
	assert.False(t, completed[0].succeeded, "succeeded")
	assert.Equal(t, now(), completed[0].createdAt, "created at")
	assert.Equal(t, now(), completed[0].lastExecutedAt, "last executed at")
	assert.Equal(t, now().Add(time.Hour).UnixMilli(), completed[0].expiresAt.UnixMilli(), "expires at")
	require.NotNil(t, completed[0].err)
	assert.Equal(t, "failure error", *completed[0].err, "error")
	assert.True(t, bytes.Equal(tk.task, completed[0].task), "task does not match")
	assert.Greater(t, completed[0].lastDuration, time.Duration(0), "last duration not set")
}

func TestDispatcherProcessTaskUnregisteredQueue(t *testing.T) {
	d := newDispatcher(t)
	d.ready = make(chan struct{}, 1)
	d.ctx = context.Background()

	// An unknown queue must not crash the worker or retain the task.
	tk := &queuedTask{
		id:        nextTaskID(),
		queue:     "missing",
		task:      []byte("x"),
		createdAt: now(),
	}
	insertTask(t, d.client.store, tk)

	assert.NotPanics(t, func() { d.processTask(d.ctx, d.ready, tk) })
	require.Len(t, getTasks(t, d.client.store), 0)
	require.Len(t, getCompletedTasks(t, d.client.store), 0)
}

func TestDispatcherFetcher(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	d := newDispatcher(t)
	d.ctx = ctx
	d.shutdownCtx = ctx
	d.ticker = time.NewTicker(time.Hour)
	d.tasks = make(chan *queuedTask, d.numWorkers)
	d.ready = make(chan struct{}, 1)
	d.trigger = make(chan struct{}, 1)
	d.availableWorkers = make(chan struct{}, d.numWorkers)

	ids := make([]string, 5)
	for i := range ids {
		ids[i] = nextTaskID()
	}

	hold := make(chan struct{}, d.numWorkers)
	for range d.numWorkers {
		d.wg.Add(1)
		go d.worker(ctx, d.shutdownCtx, d.tasks, d.availableWorkers, d.ready)
		d.availableWorkers <- struct{}{}
	}

	d.client.Register(NewQueue(func(_ context.Context, _ testTask) error {
		// Hold until the tasks are claimed.
		<-hold
		return nil
	}))

	for i := range ids {
		insertTask(t, d.client.store, &queuedTask{
			id:        ids[i],
			queue:     "test",
			task:      encode(t, &testTask{Val: "1"}),
			createdAt: now(),
		})
	}

	d.fetch(ctx, d.tasks, d.ticker, d.ready, d.trigger)

	// The first three tasks were claimed; the rest were untouched.
	rows, err := d.client.store.Query(context.Background(), "SELECT id, claimed_at FROM "+tasksTable)
	require.NoError(t, err)
	defer rows.Close()

	var rowCount int
	for rows.Next() {
		rowCount++
		var id string
		var claimedAt *time.Time
		require.NoError(t, rows.Scan(&id, &claimedAt))
		switch id {
		case ids[0], ids[1], ids[2]:
			assert.NotNil(t, claimedAt, "task %s should have been claimed", id)
		default:
			assert.Nil(t, claimedAt, "task %s should not have been claimed", id)
		}
	}
	require.NoError(t, rows.Err())
	assert.Equal(t, 5, rowCount, "rows")

	// Release processing and wait for the workers.
	for range d.numWorkers {
		hold <- struct{}{}
	}
	for range d.numWorkers {
		<-d.availableWorkers
	}

	// The workers completed the first three tasks.
	taskIDsExist(t, d.client.store, ids[3:5])
	completedTaskIDsExist(t, d.client.store, ids[0:3])

	// Completed tasks have an incremented attempt count.
	for _, task := range getCompletedTasks(t, d.client.store) {
		assert.Equal(t, 1, task.attempts, "attempts")
	}

	// A ready signal was sent for the next task.
	waitForChan(t, d.ready)

	// A scheduled task resets the ticker.
	deleteTasks(t, d.client.store)
	scheduledID := nextTaskID()
	insertTask(t, d.client.store, &queuedTask{
		id:        scheduledID,
		queue:     "test",
		task:      encode(t, &testTask{Val: "1"}),
		createdAt: now(),
		waitUntil: pointer(now().Add(100 * time.Millisecond)),
	})
	hold <- struct{}{}
	d.availableWorkers <- struct{}{}
	d.fetch(ctx, d.tasks, d.ticker, d.ready, d.trigger)

	select {
	case <-d.ticker.C:
	case <-time.After(250 * time.Millisecond):
		t.Error("ticker was not reset")
	}
}

// newDispatcher builds a dispatcher around a new client.
func newDispatcher(t *testing.T) *dispatcher {
	t.Helper()
	return &dispatcher{
		numWorkers: 3,
		log:        &noLogger{},
		client:     mustNewClient(t),
	}
}
