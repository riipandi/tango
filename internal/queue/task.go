package queue

import (
	"context"
	"time"

	"github.com/riipandi/tango/internal/datastore"
)

type (
	// Task is a queued unit of work.
	Task interface {
		// Config returns the task's queue configuration.
		Config() QueueConfig
	}

	// TaskAddOp builds a task add operation.
	TaskAddOp struct {
		client *Client
		ctx    context.Context
		tasks  []Task
		wait   *time.Time
		exec   datastore.Executor
	}
)

// Ctx sets the operation context.
func (t *TaskAddOp) Ctx(ctx context.Context) *TaskAddOp {
	t.ctx = ctx
	return t
}

// At sets the earliest execution time.
func (t *TaskAddOp) At(processAt time.Time) *TaskAddOp {
	t.wait = &processAt
	return t
}

// Wait delays execution by duration.
func (t *TaskAddOp) Wait(duration time.Duration) *TaskAddOp {
	t.At(now().Add(duration))
	return t
}

// Executor adds tasks through an existing transaction. The caller must
// commit it and call Client.Notify afterward.
func (t *TaskAddOp) Executor(exec datastore.Executor) *TaskAddOp {
	t.exec = exec
	return t
}

// Save persists the tasks, returning the task IDs.
func (t *TaskAddOp) Save() ([]string, error) {
	return t.client.save(t)
}
