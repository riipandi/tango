package antree

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"uuid"
)

// Executor is the minimal query surface the queue needs; both a pgx pool and
// an open pgx transaction satisfy it.
type Executor interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// queuedTask is a row in the queue_tasks table.
type queuedTask struct {
	id             string
	queue          string
	task           []byte
	attempts       int
	waitUntil      *time.Time
	createdAt      time.Time
	lastExecutedAt *time.Time
	claimedAt      *time.Time
}

// insertTx inserts a queued task as part of the given executor. The ID is
// generated in the app (UUIDv7) so callers can reference the task before the
// insert commits, and because it is time-sortable.
func (t *queuedTask) insertTx(ctx context.Context, exec Executor) error {
	if len(t.id) == 0 {
		t.id = uuid.NewV7().String()
	}

	if t.createdAt.IsZero() {
		t.createdAt = now()
	}

	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto("queue_tasks")
	ib.Cols("id", "created_at", "queue", "task", "attempts", "wait_until")
	ib.Values(t.id, t.createdAt, t.queue, t.task, t.attempts, t.waitUntil)

	query, args := ib.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: insert task: %w", err)
	}
	return nil
}

// deleteTx deletes a queued task as part of the given executor.
func (t *queuedTask) deleteTx(ctx context.Context, exec Executor) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom("queue_tasks")
	db.Where(db.Equal("id", t.id))

	query, args := db.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: delete task: %w", err)
	}
	return nil
}

// fail releases a claimed task back to the queue and schedules it for
// another execution.
func (t *queuedTask) fail(ctx context.Context, exec Executor, waitUntil time.Time) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update("queue_tasks")
	ub.Set(
		ub.Assign("claimed_at", nil),
		ub.Assign("wait_until", waitUntil),
		ub.Assign("last_executed_at", t.lastExecutedAt),
	)
	ub.Where(ub.Equal("id", t.id))

	query, args := ub.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: fail task: %w", err)
	}
	return nil
}

// queuedTasks is a slice of queued tasks.
type queuedTasks []*queuedTask

// claim marks the tasks as claimed for execution.
func (t queuedTasks) claim(ctx context.Context, exec Executor) error {
	if len(t) == 0 {
		return nil
	}

	ids := make([]any, 0, len(t))
	for _, task := range t {
		ids = append(ids, task.id)
	}

	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update("queue_tasks")
	ub.Set("attempts = attempts + 1", ub.Assign("claimed_at", time.Now()))
	ub.Where(ub.In("id", ids...))

	query, args := ub.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: claim tasks: %w", err)
	}
	return nil
}

// scanQueuedTasks loads queued tasks from the database using the given query.
func scanQueuedTasks(ctx context.Context, exec Executor, query string, args ...any) (queuedTasks, error) {
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("antree: select tasks: %w", err)
	}
	defer rows.Close()

	tasks := make(queuedTasks, 0)
	for rows.Next() {
		var task queuedTask
		if err := rows.Scan(&task.id, &task.queue, &task.task, &task.attempts, &task.waitUntil, &task.createdAt, &task.lastExecutedAt, &task.claimedAt); err != nil {
			return nil, fmt.Errorf("antree: scan task: %w", err)
		}
		tasks = append(tasks, &task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("antree: select tasks: %w", err)
	}

	return tasks, nil
}

// getScheduledTasks loads the next tasks up for execution, ordered by
// execution time. Tasks are not filtered by readiness; the deadline includes
// tasks whose claim expired, so the dispatcher can release them again.
func getScheduledTasks(ctx context.Context, exec Executor, deadline time.Time, limit int) (queuedTasks, error) {
	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("id", "queue", "task", "attempts", "wait_until", "created_at", "last_executed_at", "NULL")
	sb.From("queue_tasks")
	sb.Where(sb.Or("claimed_at IS NULL", sb.LT("claimed_at", deadline)))
	// Ready tasks (NULL wait_until) must come first, ahead of scheduled ones.
	sb.OrderBy("wait_until ASC NULLS FIRST", "id ASC")
	sb.Limit(limit)

	query, args := sb.Build()
	return scanQueuedTasks(ctx, exec, query, args...)
}

// completedTask is a row in the queue_tasks_completed table.
type completedTask struct {
	id             string
	queue          string
	task           []byte
	attempts       int
	succeeded      bool
	lastDuration   time.Duration
	expiresAt      *time.Time
	createdAt      time.Time
	lastExecutedAt time.Time
	err            *string
}

// insertTx inserts a completed task as part of the given executor.
func (t *completedTask) insertTx(ctx context.Context, exec Executor) error {
	ib := sqlbuilder.PostgreSQL.NewInsertBuilder()
	ib.InsertInto("queue_tasks_completed")
	ib.Cols("id", "created_at", "queue", "last_executed_at", "attempts", "last_duration_micro", "succeeded", "task", "expires_at", "error")
	ib.Values(
		t.id,
		t.createdAt,
		t.queue,
		t.lastExecutedAt,
		t.attempts,
		t.lastDuration.Microseconds(),
		t.succeeded,
		t.task,
		t.expiresAt,
		t.err,
	)

	query, args := ib.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: insert completed task: %w", err)
	}
	return nil
}

// scanCompletedTasks loads completed tasks from the database using the given query.
func scanCompletedTasks(ctx context.Context, exec Executor, query string, args ...any) ([]*completedTask, error) {
	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("antree: select completed tasks: %w", err)
	}
	defer rows.Close()

	tasks := make([]*completedTask, 0)
	for rows.Next() {
		var task completedTask
		var lastDuration int64
		if err := rows.Scan(&task.id, &task.createdAt, &task.queue, &task.lastExecutedAt, &task.attempts, &lastDuration, &task.succeeded, &task.task, &task.expiresAt, &task.err); err != nil {
			return nil, fmt.Errorf("antree: scan completed task: %w", err)
		}
		task.lastDuration = time.Duration(lastDuration) * time.Microsecond
		tasks = append(tasks, &task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("antree: select completed tasks: %w", err)
	}

	return tasks, nil
}

// deleteExpiredCompletedTasks removes completed tasks whose expiration is in the past.
func deleteExpiredCompletedTasks(ctx context.Context, exec Executor) error {
	db := sqlbuilder.PostgreSQL.NewDeleteBuilder()
	db.DeleteFrom("queue_tasks_completed")
	db.Where("expires_at IS NOT NULL", db.LTE("expires_at", time.Now()))

	query, args := db.Build()
	if _, err := exec.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("antree: delete expired completed tasks: %w", err)
	}
	return nil
}
