package antree

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

type (
	// Task represents a task that will be placed in a queue for execution.
	Task interface {
		// Config returns the configuration of the queue this task is placed in.
		Config() QueueConfig
	}

	// TaskAddOp facilitates adding tasks to queues.
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

// Wait instructs the task to wait at least the given duration before execution.
func (t *TaskAddOp) Wait(duration time.Duration) *TaskAddOp {
	t.At(now().Add(duration))
	return t
}

// Tx adds the tasks as part of the given transaction. The caller owns the
// transaction and must commit it, then call Client.Notify so the dispatcher
// becomes aware of the new tasks; without polling, it cannot know when a
// transaction is committed.
func (t *TaskAddOp) Tx(tx pgx.Tx) *TaskAddOp {
	t.tx = tx
	return t
}

// Save persists the tasks so they are queued for execution, returning the task IDs.
func (t *TaskAddOp) Save() ([]string, error) {
	return t.client.save(t)
}
