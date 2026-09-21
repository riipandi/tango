package queue

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
	"uuid"

	"github.com/riipandi/tango/internal/datastore"
)

// ctxKeyClient stores the client in a processor's context, so a task can
// enqueue the task that follows it.
type ctxKeyClient struct{}

// now returns the current time in a way tests can override.
var now = func() time.Time { return time.Now() }

type (
	// Client registers queues, adds tasks to them, and runs the dispatcher
	// that executes them. It is built once per process over the shared
	// Postgres pool, and lives and dies with the serve command.
	Client struct {
		// store is the shared database the tasks live in.
		store Store
		// log is the process logger; the queue never builds its own.
		log *slog.Logger
		// queues holds the registered queues tasks can be added to.
		queues queues
		// buffers is a pool of byte buffers for payload encoding.
		buffers sync.Pool
		// dispatcher claims tasks and hands them to the workers.
		dispatcher dispatcher
	}

	// ClientConfig contains configuration for the Client.
	ClientConfig struct {
		// Store is the shared Postgres pool, read and written through the
		// Querier surface.
		Store Store
		// Logger is the process logger. Nil discards every line, which is
		// what a short-lived test wants.
		Logger *slog.Logger
		// NumWorkers is the number of goroutines that execute queued tasks
		// concurrently.
		NumWorkers int
		// ReleaseAfter is the duration after which a claimed task is released
		// back to the queue if it has not finished. It should be much higher
		// than every queue's Timeout, existing as the fail-safe for a worker
		// lost to a crash or a network partition.
		ReleaseAfter time.Duration
	}
)

// Client lifecycle errors, so a config that cannot run is told apart from a
// database that cannot be reached.
var (
	errMissingStore = errors.New("queue: missing store")
	errNoWorkers    = errors.New("queue: at least one worker required")
	errNoRelease    = errors.New("queue: release duration must be greater than zero")
)

// NewClient initializes a new Client. The dispatcher is built but not run:
// Start runs it.
func NewClient(cfg ClientConfig) (*Client, error) {
	switch {
	case cfg.Store == nil:
		return nil, errMissingStore
	case cfg.NumWorkers < 1:
		return nil, errNoWorkers
	case cfg.ReleaseAfter <= 0:
		return nil, errNoRelease
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}

	c := &Client{
		store:   cfg.Store,
		log:     cfg.Logger,
		queues:  queues{registry: make(map[string]Queue)},
		buffers: sync.Pool{New: func() any { return bytes.NewBuffer(nil) }},
	}
	c.dispatcher.init(c, cfg.NumWorkers, cfg.ReleaseAfter)
	return c, nil
}

// Register registers a queue so tasks can be added to it. It panics when the
// name is empty or already registered: both are wiring bugs, and a queue
// registered twice would silently split its tasks' fate.
func (c *Client) Register(queue Queue) {
	c.queues.add(queue)
}

// Add starts an operation to add one or many tasks.
func (c *Client) Add(tasks ...Task) *TaskAddOp {
	return &TaskAddOp{client: c, tasks: tasks}
}

// Start starts the dispatcher so queued tasks are executed in the background.
// To gracefully shut it down, call Stop; to hard-stop it, cancel the context.
func (c *Client) Start(ctx context.Context) {
	c.dispatcher.start(ctx)
}

// Stop attempts to gracefully shut down the dispatcher before the provided
// context is cancelled, waiting for the workers to finish their tasks. True
// is returned when every worker finished in time.
func (c *Client) Stop(ctx context.Context) bool {
	return c.dispatcher.stop(ctx)
}

// Shutdown stops the dispatcher for the container's shutdown walk: a job in
// flight finishes when it can, and a worker that did not finish in time is
// reported rather than hidden, because the task comes back on release.
func (c *Client) Shutdown(ctx context.Context) {
	if c.dispatcher.stop(ctx) {
		return
	}
	c.log.WarnContext(ctx, "queue: shutdown left tasks running")
}

// Notify tells the dispatcher that new tasks were added. It is only needed
// for tasks added through an open transaction (TaskAddOp.Executor): the
// dispatcher cannot observe a commit it does not own.
func (c *Client) Notify() {
	c.dispatcher.notify()
}

// Status returns the state of a task with a given ID. A completed task that
// its queue did not retain reads as TaskStatusNotFound: the record is gone,
// and the queue said that is the same as never having run.
func (c *Client) Status(ctx context.Context, taskID uuid.UUID) (TaskStatus, error) {
	return taskStatus(ctx, c.store, taskID)
}

// Pending reports how many unclaimed tasks a queue holds. A recurring job
// reads it before seeding itself, so a restart never adds a second schedule.
func (c *Client) Pending(ctx context.Context, queue string) (int64, error) {
	return countPending(ctx, c.store, queue)
}

// Flush deletes all pending tasks and reports how many were removed. Claimed
// tasks are untouched: they are in flight or awaiting release, and both
// states finish their lifecycle normally.
func (c *Client) Flush(ctx context.Context) (int64, error) {
	return flushPending(ctx, c.store)
}

// FlushCompleted deletes every completed task record, retention
// notwithstanding, and reports how many were removed.
func (c *Client) FlushCompleted(ctx context.Context) (int64, error) {
	return flushCompleted(ctx, c.store)
}

// DeleteExpiredCompleted removes the completed records their retention has
// expired and reports how many were removed. It is the maintenance the
// cleanup job schedules; nothing else calls it.
func (c *Client) DeleteExpiredCompleted(ctx context.Context) (int64, error) {
	return deleteExpiredCompleted(ctx, c.store, now())
}

// FromContext returns the client a processor's context carries, so a task can
// enqueue the task that follows it. Nil outside a processor.
func FromContext(ctx context.Context) *Client {
	if client, ok := ctx.Value(ctxKeyClient{}).(*Client); ok {
		return client
	}
	return nil
}

// save encodes and inserts the tasks of one operation. Without an executor
// the inserts run in their own transaction, which save commits and then
// notifies the dispatcher about; with one, the inserts join the caller's
// transaction and the notification is the caller's to send after the commit.
func (c *Client) save(op *TaskAddOp) ([]string, error) {
	if op.ctx == nil {
		op.ctx = context.Background()
	}

	buf, _ := c.buffers.Get().(*bytes.Buffer)
	defer c.buffers.Put(buf)

	tasks := make([]*taskRow, len(op.tasks))
	ids := make([]string, len(op.tasks))
	for i, task := range op.tasks {
		if err := encode(buf, task); err != nil {
			return nil, err
		}
		row, err := newTask(task, buf.Bytes(), op.wait)
		if err != nil {
			return nil, err
		}
		tasks[i] = row
		ids[i] = row.ID.String()
	}

	if op.executor != nil {
		if err := insertTasks(op.ctx, op.executor, tasks); err != nil {
			return nil, err
		}
		return ids, nil
	}

	err := c.store.WithTx(op.ctx, func(ctx context.Context, tx datastore.Querier) error {
		return insertTasks(ctx, tx, tasks)
	})
	if err != nil {
		return nil, err
	}

	// The transaction is committed, so the tasks are visible and the
	// dispatcher can claim them.
	c.Notify()
	return ids, nil
}

// encode serializes a task payload into a pooled buffer.
func encode(buf *bytes.Buffer, task Task) error {
	buf.Reset()
	if err := json.MarshalEncode(jsontext.NewEncoder(buf), task); err != nil {
		return fmt.Errorf("queue: encode task: %w", err)
	}
	return nil
}
