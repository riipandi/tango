package antree

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"uuid"
)

func TestQueuedTaskInsertScanRoundtrip(t *testing.T) {
	store := newPool(t)

	task := &queuedTask{
		queue:     "test",
		task:      []byte("payload"),
		attempts:  2,
		waitUntil: pointer(now()),
	}
	insertTask(t, store, task)

	// Generated UUIDv7 ID and defaulted creation time.
	_, err := uuid.Parse(task.id)
	require.NoError(t, err)
	assert.Equal(t, now(), task.createdAt)

	got := getTasks(t, store)
	require.Len(t, got, 1)
	isTask(t, *task, *got[0])
}

func TestQueuedTaskClaim(t *testing.T) {
	store := newPool(t)
	ctx := context.Background()

	tasks := queuedTasks{
		{id: nextTaskID(), queue: "test", task: []byte("x")},
		{id: nextTaskID(), queue: "test", task: []byte("x")},
	}
	for _, task := range tasks {
		insertTask(t, store, task)
	}

	// Claiming an empty set is a no-op.
	require.NoError(t, queuedTasks{}.claim(ctx, store))

	require.NoError(t, tasks.claim(ctx, store))
	got := getTasks(t, store)
	for _, task := range got {
		assert.NotNil(t, task.claimedAt, "task %s should be claimed", task.id)
		assert.Equal(t, 1, task.attempts, "claim increments attempts")
	}
}

func TestQueuedTaskFail(t *testing.T) {
	store := newPool(t)
	ctx := context.Background()

	task := &queuedTask{id: nextTaskID(), queue: "test", task: []byte("x"), attempts: 1}
	insertTask(t, store, task)

	next := now().Add(time.Minute)
	require.NoError(t, task.fail(ctx, store, next))

	got := getTasks(t, store)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].waitUntil)
	assert.Equal(t, next.UnixMilli(), got[0].waitUntil.UnixMilli())
	assert.Nil(t, got[0].claimedAt, "claim released")
}

func TestGetScheduledTasks(t *testing.T) {
	store := newPool(t)

	waited := &queuedTask{id: nextTaskID(), queue: "test", task: []byte("x"), waitUntil: pointer(now().Add(time.Hour))}
	ready := &queuedTask{id: nextTaskID(), queue: "test", task: []byte("x")}
	insertTask(t, store, waited)
	insertTask(t, store, ready)

	got, err := getScheduledTasks(context.Background(), store, now().Add(-time.Second), 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	// Ready tasks (NULL wait_until) come first.
	assert.Equal(t, ready.id, got[0].id)
	assert.Equal(t, waited.id, got[1].id)
}

func TestCompletedTaskInsertScanRoundtrip(t *testing.T) {
	store := newPool(t)

	task := completedTask{
		id:             nextTaskID(),
		queue:          "test",
		task:           []byte("payload"),
		attempts:       3,
		succeeded:      false,
		lastDuration:   1500 * time.Microsecond,
		expiresAt:      pointer(now().Add(time.Hour)),
		createdAt:      now(),
		lastExecutedAt: now(),
		err:            pointer("boom"),
	}
	insertCompleted(t, store, task)

	got := getCompletedTasks(t, store)
	require.Len(t, got, 1)
	assert.Equal(t, task.id, got[0].id)
	assert.Equal(t, task.queue, got[0].queue)
	assert.Equal(t, task.attempts, got[0].attempts)
	assert.False(t, got[0].succeeded)
	assert.Equal(t, task.lastDuration, got[0].lastDuration)
	require.NotNil(t, got[0].expiresAt)
	assert.Equal(t, now().Add(time.Hour).UnixMilli(), got[0].expiresAt.UnixMilli())
	require.NotNil(t, got[0].err)
	assert.Equal(t, "boom", *got[0].err)
	assert.True(t, string(got[0].task) == "payload", "task bytes")
}

func TestDeleteExpiredCompletedTasks(t *testing.T) {
	store := newPool(t)

	insertCompleted(t, store, completedTask{id: nextTaskID(), queue: "test", createdAt: now(), lastExecutedAt: now(), expiresAt: pointer(now().Add(-time.Minute))})
	insertCompleted(t, store, completedTask{id: nextTaskID(), queue: "test", createdAt: now(), lastExecutedAt: now(), expiresAt: pointer(now().Add(time.Hour))})
	insertCompleted(t, store, completedTask{id: nextTaskID(), queue: "test", createdAt: now(), lastExecutedAt: now()})

	require.NoError(t, deleteExpiredCompletedTasks(context.Background(), store))

	tasks := getCompletedTasks(t, store)
	require.Len(t, tasks, 2)
	require.NotNil(t, tasks[0].expiresAt, "still retained")
	assert.Nil(t, tasks[1].expiresAt, "kept forever")
}
