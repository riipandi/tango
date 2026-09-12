// Package antree provides type-safe, persistent task queues backed by
// Postgres that run within the application process instead of an external
// message broker.
//
// A port of github.com/mikestefanello/backlite (MIT license), adapted to
// Postgres as a single self-contained package.
package antree

import (
	"bytes"
	"context"
	"errors"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// now returns the current time in a way that tests can override.
var now = func() time.Time {
	return time.Now()
}

type (
	// Client registers queues and adds tasks to them for execution.
	Client struct {
		db     *pgxpool.Pool
		log    Logger
		queues queues

		// buffers reuses encoding buffers across saves.
		buffers sync.Pool

		dispatcher Dispatcher
	}

	// ClientConfig contains configuration for the Client.
	ClientConfig struct {
		DB *pgxpool.Pool

		// Logger logs task execution. Omit to disable logging.
		Logger Logger

		// NumWorkers is the number of goroutines executing queued tasks.
		NumWorkers int

		// ReleaseAfter reclaims a claimed task that never finished executing.
		// A fail-safe for stuck tasks; much higher than any queue Timeout.
		ReleaseAfter time.Duration

		// CleanupInterval removes expired completed tasks. If omitted,
		// retention durations are never enforced.
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

// FromContext returns the Client stored in a queue processor context, so
// processors can add additional tasks.
func FromContext(ctx context.Context) *Client {
	if c, ok := ctx.Value(ctxKeyClient{}).(*Client); ok {
		return c
	}
	return nil
}

// NewClient initializes a new Client.
func NewClient(cfg ClientConfig) (*Client, error) {
	switch {
	case cfg.DB == nil:
		return nil, errors.New("missing database")
	case cfg.NumWorkers < 1:
		return nil, errors.New("at least one worker required")
	case cfg.ReleaseAfter <= 0:
		return nil, errors.New("release duration must be greater than zero")
	}

	if cfg.Logger == nil {
		cfg.Logger = &noLogger{}
	}

	c := &Client{
		db:     cfg.DB,
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

// Start executes queued tasks in the background. Cancel the context for a
// hard stop; call Stop for a graceful one.
func (c *Client) Start(ctx context.Context) {
	c.dispatcher.Start(ctx)
}

// Stop shuts down gracefully, waiting until the context is cancelled or all
// workers finish their tasks. True when all workers completed in time.
func (c *Client) Stop(ctx context.Context) bool {
	return c.dispatcher.Stop(ctx)
}

// Notify tells the dispatcher that new tasks were added. Only needed when
// tasks are added within a caller-managed transaction (see TaskAddOp.Tx).
func (c *Client) Notify() {
	c.dispatcher.Notify()
}

// Flush deletes all pending (unclaimed) tasks and returns how many were
// removed. Claimed tasks — in flight or awaiting release — are untouched.
func (c *Client) Flush(ctx context.Context) (int64, error) {
	return flushTasks(ctx, c.db)
}

// FlushCompleted deletes all completed task records and returns how many
// were removed, bypassing retention expiry.
func (c *Client) FlushCompleted(ctx context.Context) (int64, error) {
	return flushCompletedTasks(ctx, c.db)
}

// save persists a task add operation and returns the task IDs.
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
	insert := func(exec Executor) error {
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

	if op.tx != nil {
		// The caller owns the transaction and commits it, then notifies us.
		err = insert(op.tx)
	} else {
		// We own the transaction: roll back on failure, commit, notify.
		var tx pgx.Tx
		if tx, err = op.client.db.Begin(op.ctx); err != nil {
			return nil, err
		}
		defer func() {
			if err == nil {
				return
			}
			if rollbackErr := tx.Rollback(op.ctx); rollbackErr != nil {
				c.log.Error("failed to rollback task creation transaction", "error", rollbackErr)
			}
		}()

		if err = insert(tx); err != nil {
			return nil, err
		}
		if err = tx.Commit(op.ctx); err != nil {
			return nil, err
		}
		c.Notify()
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
	// Queued tasks: pending or running.
	running := sqlbuilder.PostgreSQL.NewSelectBuilder()
	running.Select("claimed_at IS NOT NULL")
	running.From(tasksTable)
	running.Where(running.Equal("id", taskID))

	var claimed bool
	runningQuery, runningArgs := running.Build()
	err := c.db.QueryRow(ctx, runningQuery, runningArgs...).Scan(&claimed)
	switch {
	case err == nil:
		if claimed {
			return TaskStatusRunning, nil
		}
		return TaskStatusPending, nil
	case errors.Is(err, pgx.ErrNoRows):
		// Fall through to completed tasks.
	default:
		return 0, err
	}

	// Completed tasks: success or failure.
	succeeded := sqlbuilder.PostgreSQL.NewSelectBuilder()
	succeeded.Select("error IS NULL")
	succeeded.From(completedTasksTable)
	succeeded.Where(succeeded.Equal("id", taskID))

	var success bool
	succeededQuery, succeededArgs := succeeded.Build()
	if err := c.db.QueryRow(ctx, succeededQuery, succeededArgs...).Scan(&success); err != nil {
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
