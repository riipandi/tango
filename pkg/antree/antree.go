// Package antree provides type-safe, persistent, embedded task queues backed
// by Postgres (the queue_tasks tables) that run within the application process
// instead of an external message broker.
//
// This package is a port of github.com/mikestefanello/backlite (MIT license),
// adapted to Postgres and restructured as a single self-contained package with
// no dependencies outside pkg/.
package antree

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// now returns the current time in a way that tests can override.
var now = func() time.Time {
	return time.Now()
}

type (
	// Client is used to register queues and add tasks to them for execution.
	Client struct {
		// db stores the database used for storing tasks.
		db *pgxpool.Pool

		// log is the logger.
		log Logger

		// queues stores the registered queues which tasks can be added to.
		queues queues

		// buffers is a pool of byte buffers for more efficient encoding.
		buffers sync.Pool

		// dispatcher fetches queued tasks and hands them to the workers.
		dispatcher Dispatcher
	}

	// ClientConfig contains configuration for the Client.
	ClientConfig struct {
		// DB is the Postgres connection pool used for storing tasks.
		DB *pgxpool.Pool

		// Logger logs task execution. Omit to disable logging.
		Logger Logger

		// NumWorkers is the number of goroutines opened to execute queued tasks
		// concurrently.
		NumWorkers int

		// ReleaseAfter is the duration after which a claimed task is released back
		// to its queue if it never finished executing. This should be much higher
		// than the timeout setting of each queue and exists as a fail-safe for
		// stuck tasks.
		ReleaseAfter time.Duration

		// CleanupInterval is how often the database is cleaned of expired completed
		// tasks. If omitted, retention durations are never enforced.
		CleanupInterval time.Duration
	}

	// ctxKeyClient is used to store a Client in a context.
	ctxKeyClient struct{}

	// TaskStatus describes the state of a task.
	TaskStatus int
)

const (
	// TaskStatusPending indicates the task is awaiting execution.
	TaskStatusPending TaskStatus = iota

	// TaskStatusRunning indicates the task is being executed.
	TaskStatusRunning

	// TaskStatusSuccess indicates the task completed successfully.
	TaskStatusSuccess

	// TaskStatusFailure indicates the task execution failed.
	TaskStatusFailure

	// TaskStatusNotFound indicates the task was not found in the database.
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

// Register registers a queue so tasks can be added to it.
// Panics if a queue with the same name is already registered.
func (c *Client) Register(queue Queue) {
	c.queues.add(queue)
}

// Add starts an operation to add one or many tasks.
func (c *Client) Add(tasks ...Task) *TaskAddOp {
	return &TaskAddOp{client: c, tasks: tasks}
}

// Start starts the dispatcher so queued tasks execute automatically in the
// background. Call Stop for a graceful shutdown, or cancel the provided
// context for a hard stop.
func (c *Client) Start(ctx context.Context) {
	c.dispatcher.Start(ctx)
}

// Stop gracefully shuts down the dispatcher, waiting until the given context is
// cancelled or all workers finish their tasks. True is returned when all
// workers completed their tasks prior to shutting down.
func (c *Client) Stop(ctx context.Context) bool {
	return c.dispatcher.Stop(ctx)
}

// Notify tells the dispatcher that new tasks were added. Only needed when
// tasks are added within a caller-managed transaction (see TaskAddOp.Tx).
func (c *Client) Notify() {
	c.dispatcher.Notify()
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
			if err = json.NewEncoder(buf).Encode(t); err != nil {
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
		// We own the transaction: roll it back on failure, commit, notify.
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
	running.From("queue_tasks")
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
	succeeded.From("queue_tasks_completed")
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
