package queue

import (
	"context"
	"time"

	"github.com/riipandi/tango/internal/datastore"
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
		exec   datastore.Executor
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

// Executor adds the tasks through the given executor — typically an
// open transaction (pgx.Tx from Pool().Begin, or the datastore WithTx
// callback) so the enqueue joins the caller's transaction. The caller
// owns the transaction and must commit it, then call Client.Notify —
// the dispatcher cannot know when a transaction commits without polling.
func (t *TaskAddOp) Executor(exec datastore.Executor) *TaskAddOp {
	t.exec = exec
	return t
}

// Save persists the tasks, returning the task IDs.
func (t *TaskAddOp) Save() ([]string, error) {
	return t.client.save(t)
}
