// Package queue provides the built-in, type-safe task queue backed by Postgres.
package queue

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"

	"github.com/riipandi/tango/internal/datastore"
)

// now is replaceable so tests can control time.
var now = func() time.Time {
	return time.Now()
}

type (
	// Client registers queues and adds tasks to them for execution.
	Client struct {
		store  datastore.Store
		log    Logger
		queues queues

		// buffers reuses encoding buffers.
		buffers sync.Pool

		dispatcher Dispatcher
	}

	// ClientConfig contains configuration for the Client.
	ClientConfig struct {
		// Store is the shared Postgres backend.
		Store datastore.Store

		// Logger logs task execution. Nil disables logging.
		Logger Logger

		// NumWorkers is the number of goroutines executing queued tasks.
		NumWorkers int

		// ReleaseAfter reclaims tasks that never finish.
		ReleaseAfter time.Duration

		// CleanupInterval removes expired completed tasks.
		CleanupInterval time.Duration
	}

	ctxKeyClient struct{}

	// TaskStatus describes the state of a task.
	TaskStatus int
)

const (
	TaskStatusPending TaskStatus = iota
	TaskStatusRunning
	TaskStatusSuccess
	TaskStatusFailure
	TaskStatusNotFound
)

// FromContext returns the Client stored in a processor context.
func FromContext(ctx context.Context) *Client {
	if c, ok := ctx.Value(ctxKeyClient{}).(*Client); ok {
		return c
	}
	return nil
}

// NewClient creates a Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	switch {
	case cfg.Store == nil:
		return nil, errors.New("missing database store")
	case cfg.NumWorkers < 1:
		return nil, errors.New("at least one worker required")
	case cfg.ReleaseAfter <= 0:
		return nil, errors.New("release duration must be greater than zero")
	}

	if cfg.Logger == nil {
		cfg.Logger = &noLogger{}
	}

	c := &Client{
		store:  cfg.Store,
		log:    cfg.Logger,
		queues: queues{registry: make(map[string]Queue)},
		buffers: sync.Pool{
			New: func() any { return bytes.NewBuffer(nil) },
		},
	}

	c.dispatcher = &dispatcher{
		client:          c,
		log:             cfg.Logger,
		numWorkers:      cfg.NumWorkers,
		releaseAfter:    cfg.ReleaseAfter,
		cleanupInterval: cfg.CleanupInterval,
	}

	return c, nil
}

// Register registers a queue. Panics on a duplicate or missing name.
func (c *Client) Register(queue Queue) {
	c.queues.add(queue)
}

// Add starts an operation to add one or many tasks.
func (c *Client) Add(tasks ...Task) *TaskAddOp {
	return &TaskAddOp{client: c, tasks: tasks}
}

// Start executes queued tasks in the background.
func (c *Client) Start(ctx context.Context) {
	c.dispatcher.Start(ctx)
}

// Stop shuts down gracefully and reports whether workers finished in time.
func (c *Client) Stop(ctx context.Context) bool {
	return c.dispatcher.Stop(ctx)
}

// Notify tells the dispatcher that new tasks were added in another transaction.
func (c *Client) Notify() {
	c.dispatcher.Notify()
}

// Flush deletes pending, unclaimed tasks and returns the count.
func (c *Client) Flush(ctx context.Context) (int64, error) {
	return flushTasks(ctx, c.store)
}

// FlushCompleted deletes all completed task records and returns the count.
func (c *Client) FlushCompleted(ctx context.Context) (int64, error) {
	return flushCompletedTasks(ctx, c.store)
}

// save persists a task add operation.
func (c *Client) save(op *TaskAddOp) ([]string, error) {
	var err error

	buf, _ := c.buffers.Get().(*bytes.Buffer)
	if buf == nil {
		buf = bytes.NewBuffer(nil)
	}
	defer func() {
		buf.Reset()
		c.buffers.Put(buf)
	}()

	if op.ctx == nil {
		op.ctx = context.Background()
	}

	ids := make([]string, len(op.tasks))
	insert := func(exec datastore.Executor) error {
		for i, t := range op.tasks {
			buf.Reset()
			if err = jsonv2.MarshalWrite(buf, t); err != nil {
				return err
			}

			row := &queuedTask{
				queue:     t.Config().Name,
				task:      buf.Bytes(),
				waitUntil: op.wait,
				createdAt: now(),
			}
			if err = row.insertTx(op.ctx, exec); err != nil {
				return err
			}
			ids[i] = row.id
		}
		return nil
	}

	if op.exec != nil {
		// The caller owns the transaction and commits it, then notifies us.
		err = insert(op.exec)
	} else {
		// We own the transaction: WithTx rolls back on failure and
		// commits on success; notify after.
		err = op.client.store.WithTx(op.ctx, func(exec datastore.Executor) error {
			return insert(exec)
		})
		if err == nil {
			c.Notify()
		}
	}
	if err != nil {
		return nil, err
	}

	return ids, nil
}

// Status returns the status of the task with the given ID. If the queue does
// not retain completed tasks, TaskStatusNotFound is returned for completed
// tasks instead of TaskStatusSuccess or TaskStatusFailure.
func (c *Client) Status(ctx context.Context, taskID string) (TaskStatus, error) {
	// Check queued tasks first.
	running := sqlbuilder.PostgreSQL.NewSelectBuilder()
	running.Select("claimed_at IS NOT NULL")
	running.From(tasksTable)
	running.Where(running.Equal("id", taskID))

	var claimed bool
	runningQuery, runningArgs := running.Build()
	err := c.store.QueryRow(ctx, runningQuery, runningArgs...).Scan(&claimed)
	switch {
	case err == nil:
		if claimed {
			return TaskStatusRunning, nil
		}
		return TaskStatusPending, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Check completed tasks next.
	default:
		return 0, err
	}

	// Check completed tasks.
	succeeded := sqlbuilder.PostgreSQL.NewSelectBuilder()
	succeeded.Select("error IS NULL")
	succeeded.From(completedTasksTable)
	succeeded.Where(succeeded.Equal("id", taskID))

	var success bool
	succeededQuery, succeededArgs := succeeded.Build()
	if err := c.store.QueryRow(ctx, succeededQuery, succeededArgs...).Scan(&success); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return TaskStatusNotFound, nil
		}
		return 0, err
	}
	if success {
		return TaskStatusSuccess, nil
	}
	return TaskStatusFailure, nil
}
