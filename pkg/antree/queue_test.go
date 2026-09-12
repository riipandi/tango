package antree

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

func TestQueuesGetUnregisteredQueuePanics(t *testing.T) {
	s := &queues{}
	assert.PanicsWithValue(t,
		"queue 'test' not registered, ensure all queues are registered before calling Client.Start()",
		func() { s.get(testTask{}.Config().Name) },
	)
}

func TestQueuesAddMissingNamePanics(t *testing.T) {
	s := &queues{registry: make(map[string]Queue)}
	assert.PanicsWithValue(t, "queue name is missing", func() {
		s.add(NewQueue(func(_ context.Context, _ testTaskNoName) error { return nil }))
	})
}
