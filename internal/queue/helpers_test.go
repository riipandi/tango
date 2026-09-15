// Queue integration tests use the shared Postgres test container.

package queue

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain fixes the clock for deterministic time assertions.
func TestMain(m *testing.M) {
	n := time.Now().Round(time.Millisecond)
	now = func() time.Time { return n }
	os.Exit(m.Run())
}

var taskIDSeq atomic.Int64

// nextTaskID returns a unique test ID.
func nextTaskID() string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", taskIDSeq.Add(1))
}

// newPool opens Postgres with migrations applied.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, pg.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	// Clear queue rows after each test.
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM queue_tasks")
		_, _ = pool.Exec(bg, "DELETE FROM queue_tasks_completed")
	})

	return pool
}

// poolStore adapts a test pool to datastore.Store.
type poolStore struct{ pool *pgxpool.Pool }

func (p poolStore) HealthCheck(ctx context.Context) error { return p.pool.Ping(ctx) }

func (p poolStore) Close() error {
	p.pool.Close()
	return nil
}

func (p poolStore) Pool() *pgxpool.Pool { return p.pool }

func (p poolStore) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

func (p poolStore) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

func (p poolStore) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

func (p poolStore) WithTx(ctx context.Context, fn func(datastore.Executor) error) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return tx.Commit(ctx)
}

// newStore wraps the test pool in a datastore.Store.
func newStore(t *testing.T) datastore.Store {
	t.Helper()
	return poolStore{pool: newPool(t)}
}

// mustNewClient builds a client backed by test Postgres.
func mustNewClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		Store:           newStore(t),
		NumWorkers:      1,
		ReleaseAfter:    time.Hour,
		CleanupInterval: 6 * time.Hour,
	})
	require.NoError(t, err)
	return client
}

// encode serializes a value like the client.
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := jsonv2.Marshal(v)
	require.NoError(t, err)
	return b
}

// pointer returns a pointer to v.
func pointer[T any](v T) *T {
	return &v
}

// getTasks loads queued tasks ordered by ID.
func getTasks(t *testing.T, exec datastore.Executor) queuedTasks {
	t.Helper()
	tasks, err := scanQueuedTasks(context.Background(), exec,
		`SELECT id, queue, task, attempts, wait_until, created_at, last_executed_at, claimed_at
		 FROM `+tasksTable+` ORDER BY id ASC`)
	require.NoError(t, err)
	return tasks
}

// insertTask inserts a queued task.
func insertTask(t *testing.T, exec datastore.Executor, task *queuedTask) {
	t.Helper()
	require.NoError(t, task.insertTx(context.Background(), exec))
}

// deleteTasks removes queued tasks.
func deleteTasks(t *testing.T, exec datastore.Executor) {
	t.Helper()
	_, err := exec.Exec(context.Background(), "DELETE FROM "+tasksTable)
	require.NoError(t, err)
}

// getCompletedTasks loads completed tasks ordered by ID.
func getCompletedTasks(t *testing.T, exec datastore.Executor) []*completedTask {
	t.Helper()
	tasks, err := scanCompletedTasks(context.Background(), exec,
		`SELECT id, created_at, queue, last_executed_at, attempts, last_duration_micro, succeeded, task, expires_at, error
		 FROM `+completedTasksTable+` ORDER BY id ASC`)
	require.NoError(t, err)
	return tasks
}

// insertCompleted inserts a completed task.
func insertCompleted(t *testing.T, exec datastore.Executor, task completedTask) {
	t.Helper()
	require.NoError(t, task.insertTx(context.Background(), exec))
}

// deleteCompletedTasks removes completed tasks.
func deleteCompletedTasks(t *testing.T, exec datastore.Executor) {
	t.Helper()
	_, err := exec.Exec(context.Background(), "DELETE FROM "+completedTasksTable)
	require.NoError(t, err)
}

// taskIDsExist checks that IDs exist in the queue.
func taskIDsExist(t *testing.T, exec datastore.Executor, ids []string) {
	t.Helper()
	idMap := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idMap[id] = struct{}{}
	}
	for _, task := range getTasks(t, exec) {
		delete(idMap, task.id)
	}
	assert.Empty(t, idMap, "ids do not exist")
}

// completedTaskIDsExist checks that IDs exist in completed tasks.
func completedTaskIDsExist(t *testing.T, exec datastore.Executor, ids []string) {
	t.Helper()
	idMap := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		idMap[id] = struct{}{}
	}
	for _, task := range getCompletedTasks(t, exec) {
		delete(idMap, task.id)
	}
	assert.Empty(t, idMap, "ids do not exist")
}

// isTask compares a scanned task with expected values.
func isTask(t *testing.T, expected, got queuedTask) {
	t.Helper()
	assert.Equal(t, expected.queue, got.queue, "queue")
	assert.Equal(t, expected.attempts, got.attempts, "attempts")
	assert.Equal(t, expected.createdAt.UnixMilli(), got.createdAt.UnixMilli(), "created_at")
	assert.True(t, bytes.Equal(expected.task, got.task), "task bytes")

	assertOptionalTime(t, "wait_until", expected.waitUntil, got.waitUntil)
	assertOptionalTime(t, "last_executed_at", expected.lastExecutedAt, got.lastExecutedAt)
	assertOptionalTime(t, "claimed_at", expected.claimedAt, got.claimedAt)
}

func assertOptionalTime(t *testing.T, name string, expected, got *time.Time) {
	t.Helper()
	switch {
	case expected == nil && got == nil:
	case expected != nil && got != nil:
		assert.Equal(t, expected.UnixMilli(), got.UnixMilli(), name)
	default:
		t.Errorf("%s not equal: expected %v, got %v", name, expected, got)
	}
}

// wait gives background goroutines time to run.
func wait() {
	time.Sleep(100 * time.Millisecond)
}

// waitForChan waits for a signal with a timeout.
func waitForChan[T any](t *testing.T, signal chan T) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(500 * time.Millisecond):
		t.Error("signal not received")
	}
}
