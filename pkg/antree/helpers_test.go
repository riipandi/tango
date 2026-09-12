// Integration tests for the antree queue. Every test that touches the
// database runs against the shared testcontainer Postgres (real pgx pool,
// real migrations) — no in-memory or fake store involved. Tests that only
// exercise in-process logic (queue decoding, adapters) stay pure.

package antree

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain freezes the clock so tests can assert exact times.
func TestMain(m *testing.M) {
	n := time.Now().Round(time.Millisecond)
	now = func() time.Time { return n }
	os.Exit(m.Run())
}

var taskIDSeq atomic.Int64

// nextTaskID returns a fixed-shape, unique-per-call task ID for tests.
func nextTaskID() string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", taskIDSeq.Add(1))
}

// newPool opens the shared testcontainer Postgres with migrations applied.
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

	// The container is shared: drop all queue rows after each test so tests
	// never see each other's data.
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, "DELETE FROM queue_tasks")
		_, _ = pool.Exec(bg, "DELETE FROM queue_tasks_completed")
	})

	return pool
}

// mustNewClient builds a client backed by the testcontainer database.
func mustNewClient(t *testing.T) *Client {
	t.Helper()
	client, err := NewClient(ClientConfig{
		DB:              newPool(t),
		NumWorkers:      1,
		ReleaseAfter:    time.Hour,
		CleanupInterval: 6 * time.Hour,
	})
	require.NoError(t, err)
	return client
}

// encode serializes a value the same way the client does.
func encode(t *testing.T, v any) []byte {
	t.Helper()
	b := bytes.NewBuffer(nil)
	require.NoError(t, json.NewEncoder(b).Encode(v))
	return b.Bytes()
}

// pointer returns a pointer to v.
func pointer[T any](v T) *T {
	return &v
}

// getTasks loads all queued tasks ordered by ID.
func getTasks(t *testing.T, exec Executor) queuedTasks {
	t.Helper()
	tasks, err := scanQueuedTasks(context.Background(), exec,
		`SELECT id, queue, task, attempts, wait_until, created_at, last_executed_at, claimed_at
		 FROM queue_tasks ORDER BY id ASC`)
	require.NoError(t, err)
	return tasks
}

// insertTask inserts a queued task directly.
func insertTask(t *testing.T, exec Executor, task *queuedTask) {
	t.Helper()
	require.NoError(t, task.insertTx(context.Background(), exec))
}

// deleteTasks removes all queued tasks.
func deleteTasks(t *testing.T, exec Executor) {
	t.Helper()
	_, err := exec.Exec(context.Background(), "DELETE FROM queue_tasks")
	require.NoError(t, err)
}

// getCompletedTasks loads all completed tasks ordered by ID.
func getCompletedTasks(t *testing.T, exec Executor) []*completedTask {
	t.Helper()
	tasks, err := scanCompletedTasks(context.Background(), exec,
		`SELECT id, created_at, queue, last_executed_at, attempts, last_duration_micro, succeeded, task, expires_at, error
		 FROM queue_tasks_completed ORDER BY id ASC`)
	require.NoError(t, err)
	return tasks
}

// insertCompleted inserts a completed task directly.
func insertCompleted(t *testing.T, exec Executor, task completedTask) {
	t.Helper()
	require.NoError(t, task.insertTx(context.Background(), exec))
}

// deleteCompletedTasks removes all completed tasks.
func deleteCompletedTasks(t *testing.T, exec Executor) {
	t.Helper()
	_, err := exec.Exec(context.Background(), "DELETE FROM queue_tasks_completed")
	require.NoError(t, err)
}

// taskIDsExist asserts the given task IDs exist in the queued table.
func taskIDsExist(t *testing.T, exec Executor, ids []string) {
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

// completedTaskIDsExist asserts the given IDs exist in the completed table.
func completedTaskIDsExist(t *testing.T, exec Executor, ids []string) {
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

// isTask asserts a scanned queued task matches the expected values.
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

// wait sleeps briefly to let background goroutines make progress.
func wait() {
	time.Sleep(100 * time.Millisecond)
}

// waitForChan asserts a signal arrives on the channel in time.
func waitForChan[T any](t *testing.T, signal chan T) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(500 * time.Millisecond):
		t.Error("signal not received")
	}
}
