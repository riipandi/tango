package antree

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type (
	// Task is placed in a queue for execution.
	Task interface {
		// Config returns the configuration of the queue this task belongs to.
		Config() QueueConfig
	}

	// TaskAddOp adds tasks to queues.
	TaskAddOp struct {
		client *Client
		ctx    context.Context
		tasks  []Task
		wait   *time.Time
		tx     pgx.Tx
	}
)

// Ctx sets the context for the operation.
func (t *TaskAddOp) Ctx(ctx context.Context) *TaskAddOp {
	t.ctx = ctx
	return t
}

// At sets the earliest time the task may be executed.
func (t *TaskAddOp) At(processAt time.Time) *TaskAddOp {
	t.wait = &processAt
	return t
}

// Wait delays execution by the given duration.
func (t *TaskAddOp) Wait(duration time.Duration) *TaskAddOp {
	t.At(now().Add(duration))
	return t
}

// Tx adds the tasks as part of the given transaction. The caller owns the
// transaction and must commit it, then call Client.Notify — the dispatcher
// cannot know when a transaction commits without polling.
func (t *TaskAddOp) Tx(tx pgx.Tx) *TaskAddOp {
	t.tx = tx
	return t
}

// Save persists the tasks, returning the task IDs.
func (t *TaskAddOp) Save() ([]string, error) {
	return t.client.save(t)
}
