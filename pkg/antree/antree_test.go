package antree

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDispatcher replaces the real dispatcher in client tests.
type mockDispatcher struct {
	started      bool
	stopped      bool
	gracefulStop bool
	notified     bool
}

func (d *mockDispatcher) Start(_ context.Context) { d.started = true }

func (d *mockDispatcher) Stop(_ context.Context) bool {
	d.stopped = true
	return d.gracefulStop
}

func (d *mockDispatcher) Notify() { d.notified = true }

func TestNewClient(t *testing.T) {
	store := newPool(t)

	c, err := NewClient(ClientConfig{
		DB:              store,
		Logger:          slog.Default(),
		NumWorkers:      2,
		ReleaseAfter:    time.Second,
		CleanupInterval: time.Hour,
	})
	require.NoError(t, err)

	d, ok := c.dispatcher.(*dispatcher)
	require.True(t, ok, "dispatcher not set")
	assert.Equal(t, slog.Default(), c.log)
	assert.Equal(t, c, d.client, "client")
	assert.Equal(t, 2, d.numWorkers, "workers")
	assert.Equal(t, time.Second, d.releaseAfter, "release after")
	assert.Equal(t, time.Hour, d.cleanupInterval, "cleanup interval")
}

func TestNewClientDefaultLogger(t *testing.T) {
	c := mustNewClient(t)
	_, ok := c.log.(*noLogger)
	assert.True(t, ok, "log not set to noLogger")
}

func TestNewClientValidation(t *testing.T) {
	store := newPool(t)

	_, err := NewClient(ClientConfig{DB: nil, NumWorkers: 1, ReleaseAfter: time.Second})
	assert.Error(t, err)

	_, err = NewClient(ClientConfig{DB: store, NumWorkers: 0, ReleaseAfter: time.Second})
	assert.Error(t, err)

	_, err = NewClient(ClientConfig{DB: store, NumWorkers: 1, ReleaseAfter: 0})
	assert.Error(t, err)
}

func TestClientRegister(t *testing.T) {
	c := mustNewClient(t)

	q := NewQueue(func(_ context.Context, _ testTask) error { return nil })
	c.Register(q)
	assert.Panics(t, func() { c.Register(q) }, "duplicate registration")

	q = NewQueue(func(_ context.Context, _ testTaskNoName) error { return nil })
	assert.Panics(t, func() { c.Register(q) }, "missing name")
}

func TestClientAdd(t *testing.T) {
	c := mustNewClient(t)

	t1, t2 := testTask{}, testTask{}
	op := c.Add(t1, t2)
	assert.Equal(t, c, op.client, "client")
	require.Len(t, op.tasks, 2)
	assert.Equal(t, t1, op.tasks[0])
	assert.Equal(t, t2, op.tasks[1])
}

func TestClientStart(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	c.Start(context.Background())
	assert.True(t, m.started)
}

func TestClientStop(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	assert.False(t, c.Stop(context.Background()))
	assert.True(t, m.stopped)

	m.gracefulStop = true
	assert.True(t, c.Stop(context.Background()))
}

func TestClientNotify(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	c.Notify()
	assert.True(t, m.notified)
}

// TestLoggerFuncAdapter proves the LoggerFunc adapter: any log function can
// back the queue logger without this package importing the logger.
func TestLoggerFuncAdapter(t *testing.T) {
	var (
		infoMessage  string
		infoParams   []any
		errorMessage string
	)

	log := LoggerFunc(func(level, message string, params ...any) {
		switch level {
		case "error":
			errorMessage = message
		default:
			infoMessage = message
			infoParams = params
		}
	})

	log.Info("task processed", "id", "abc", "attempt", 1)
	assert.Equal(t, "task processed", infoMessage)
	assert.Equal(t, []any{"id", "abc", "attempt", 1}, infoParams)

	log.Error("task failed")
	assert.Equal(t, "task failed", errorMessage)
}

func TestClientFromContext(t *testing.T) {
	assert.Nil(t, FromContext(context.Background()))

	c := &Client{}
	ctx := context.WithValue(context.Background(), ctxKeyClient{}, c)
	assert.Equal(t, c, FromContext(ctx))
}

func TestClientStatus(t *testing.T) {
	c := mustNewClient(t)
	ctx := context.Background()

	id := nextTaskID()
	s, err := c.Status(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusNotFound, s)

	insertTask(t, c.db, &queuedTask{id: id, queue: "test", task: []byte("test")})
	s, err = c.Status(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, s)

	_, err = (queuedTasks{{id: id}}).claim(ctx, c.db, now().Add(-time.Second))
	require.NoError(t, err)
	s, err = c.Status(ctx, id)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusRunning, s)

	completedID := nextTaskID()
	insertCompleted(t, c.db, completedTask{id: completedID, queue: "test"})
	s, err = c.Status(ctx, completedID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusSuccess, s)

	failedID := nextTaskID()
	insertCompleted(t, c.db, completedTask{id: failedID, queue: "test", err: pointer("err")})
	s, err = c.Status(ctx, failedID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusFailure, s)
}
