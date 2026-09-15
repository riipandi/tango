package queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskAddOpCtx(t *testing.T) {
	op := &TaskAddOp{}
	ctx := context.Background()

	op.Ctx(ctx)
	assert.Equal(t, ctx, op.ctx)
}

func TestTaskAddOpAt(t *testing.T) {
	op := &TaskAddOp{}
	at := time.Now()

	op.At(at)
	require.NotNil(t, op.wait)
	assert.Equal(t, at, *op.wait)
}

func TestTaskAddOpWait(t *testing.T) {
	op := &TaskAddOp{}

	op.Wait(time.Hour)
	require.NotNil(t, op.wait)
	assert.Equal(t, now().Add(time.Hour), *op.wait)
}

func TestTaskAddOpExecutor(t *testing.T) {
	tx, err := newPool(t).Begin(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	op := &TaskAddOp{}
	op.Executor(tx)
	assert.Equal(t, tx, op.exec)
}

func TestTaskAddOpSaveSingle(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	tk := testTask{Val: "a"}
	ids, err := c.Add(tk).Save()
	require.NoError(t, err)
	require.Len(t, ids, 1)

	got := getTasks(t, c.store)
	require.Len(t, got, 1)
	assert.Equal(t, ids[0], got[0].id, "id")
	isTask(t, queuedTask{
		queue:     tk.Config().Name,
		task:      encode(t, tk),
		attempts:  0,
		createdAt: now(),
	}, *got[0])
	assert.True(t, m.notified, "notified")
}

func TestTaskAddOpSaveWait(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	tk := testTask{Val: "f"}
	_, err := c.Add(tk).Wait(time.Hour).Save()
	require.NoError(t, err)

	got := getTasks(t, c.store)
	require.Len(t, got, 1)
	isTask(t, queuedTask{
		queue:     tk.Config().Name,
		task:      encode(t, tk),
		attempts:  0,
		createdAt: now(),
		waitUntil: pointer(now().Add(time.Hour)),
	}, *got[0])
}

func TestTaskAddOpSaveMultiple(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	task1, task2 := testTask{Val: "b"}, testTask{Val: "c"}
	ids, err := c.Add(task1, task2).Save()
	require.NoError(t, err)
	require.Len(t, ids, 2)

	got := getTasks(t, c.store)
	require.Len(t, got, 2)
	assert.Equal(t, ids[0], got[0].id, "id.0")
	assert.Equal(t, ids[1], got[1].id, "id.1")
	isTask(t, queuedTask{
		queue:     task1.Config().Name,
		task:      encode(t, task1),
		attempts:  0,
		createdAt: now(),
	}, *got[0])
	isTask(t, queuedTask{
		queue:     task2.Config().Name,
		task:      encode(t, task2),
		attempts:  0,
		createdAt: now(),
	}, *got[1])
}

func TestTaskAddOpSaveContext(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	ctx, cancel := context.WithCancel(context.Background())
	tk := testTask{Val: "d"}

	_, err := c.Add(tk).Ctx(ctx).Save()
	require.NoError(t, err)

	cancel()
	_, err = c.Add(tk).Ctx(ctx).Save()
	assert.ErrorIs(t, err, context.Canceled)
}

func TestTaskAddOpSaveTransaction(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	tk := testTask{Val: "e"}

	// The caller owns the transaction; notify comes after commit.
	tx, err := newPool(t).Begin(t.Context())
	require.NoError(t, err)
	_, err = c.Add(tk).Executor(tx).Save()
	require.NoError(t, err)
	assert.False(t, m.notified, "must not notify before commit")
	require.NoError(t, tx.Commit(t.Context()))
	require.Len(t, getTasks(t, c.store), 1)
}

func TestTaskAddOpSaveEncodeFailure(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	tk := testTaskEncodeFail{Val: make(chan int)}

	// An encode error propagates and rolls back the caller transaction.
	tx, err := newPool(t).Begin(t.Context())
	require.NoError(t, err)
	_, err = c.Add(tk).Executor(tx).Save()
	assert.Error(t, err)
	require.NoError(t, tx.Rollback(t.Context()))
	require.Len(t, getTasks(t, c.store), 0)
}

func TestTaskAddOpSaveRollback(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	// An encode failure rolls back the client-managed transaction.
	_, err := c.Add(testTaskEncodeFail{Val: make(chan int)}).Save()
	assert.Error(t, err)
	assert.False(t, m.notified)
	require.Len(t, getTasks(t, c.store), 0)
}

func TestTaskAddOpSaveZeroTasks(t *testing.T) {
	c := mustNewClient(t)
	m := &mockDispatcher{}
	c.dispatcher = m

	ids, err := c.Add().Save()
	require.NoError(t, err)
	assert.Empty(t, ids)
	assert.True(t, m.notified, "notified")
}
