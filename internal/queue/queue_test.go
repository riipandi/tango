package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueueCannotDecode(t *testing.T) {
	q := NewQueue(func(_ context.Context, _ testTask) error { return nil })
	err := q.Process(context.Background(), []byte{1, 2, 3})
	assert.Error(t, err)
}

func TestQueueProcessTypedPayload(t *testing.T) {
	var got testTask
	q := NewQueue(func(_ context.Context, task testTask) error {
		got = task
		return nil
	})

	require.NoError(t, q.Process(context.Background(), encode(t, testTask{Val: "abc"})))
	assert.Equal(t, "abc", got.Val)
}

func TestQueuesLookupUnregistered(t *testing.T) {
	s := &queues{registry: make(map[string]Queue)}
	_, ok := s.lookup("test")
	assert.False(t, ok, "unregistered queue found")

	s.registry["test"] = NewQueue(func(_ context.Context, _ testTask) error { return nil })
	q, ok := s.lookup("test")
	assert.True(t, ok)
	assert.Equal(t, "test", q.Config().Name)
}

func TestQueuesAddMissingNamePanics(t *testing.T) {
	s := &queues{registry: make(map[string]Queue)}
	assert.PanicsWithValue(t, "queue name is missing", func() {
		s.add(NewQueue(func(_ context.Context, _ testTaskNoName) error { return nil }))
	})
}
