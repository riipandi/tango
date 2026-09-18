package queue

import (
	"context"
	"fmt"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"
)

type (
	// Queue contains tasks to be executed.
	Queue interface {
		Config() *QueueConfig

		// Process executes a payload.
		Process(ctx context.Context, payload []byte) error
	}

	// QueueConfig holds the configuration options for a queue.
	QueueConfig struct {
		// Name identifies the queue.
		Name string

		// MaxAttempts is the maximum number of execution attempts.
		MaxAttempts int

		// Timeout bounds task execution.
		Timeout time.Duration

		// Backoff is the delay before retrying a failed task.
		Backoff time.Duration

		// Retention controls completed task records. Nil disables retention.
		Retention *Retention
	}

	// Retention is the policy for retaining completed tasks.
	Retention struct {
		// Duration is how long a completed task is retained. Zero means forever.
		Duration time.Duration

		// OnlyFailed retains only failed tasks.
		OnlyFailed bool

		// Data controls payload retention. Nil retains no payload.
		Data *RetainData
	}

	// RetainData is the policy for retaining payload data of completed tasks.
	RetainData struct {
		OnlyFailed bool
	}

	// queue is a type-safe Queue implementation.
	queue[T Task] struct {
		config    *QueueConfig
		processor QueueProcessor[T]
	}

	// QueueProcessor is a generic processor callback for a queue.
	QueueProcessor[T Task] func(context.Context, T) error

	// queues is a registry of queues.
	queues struct {
		registry map[string]Queue
		sync.RWMutex
	}
)

// NewQueue creates a Queue for the given task type.
func NewQueue[T Task](processor QueueProcessor[T]) Queue {
	var task T
	cfg := task.Config()

	return &queue[T]{
		config:    &cfg,
		processor: processor,
	}
}

func (q *queue[T]) Config() *QueueConfig {
	return q.config
}

func (q *queue[T]) Process(ctx context.Context, payload []byte) error {
	var obj T
	if err := jsonv2.Unmarshal(payload, &obj); err != nil {
		return err
	}
	return q.processor(ctx, obj)
}

// add registers a queue, panicking when the name is missing or taken.
func (q *queues) add(queue Queue) {
	if len(queue.Config().Name) == 0 {
		panic("queue name is missing")
	}

	q.Lock()
	defer q.Unlock()
	if _, exists := q.registry[queue.Config().Name]; exists {
		panic(fmt.Sprintf("queue '%s' already registered", queue.Config().Name))
	}
	q.registry[queue.Config().Name] = queue
}

// lookup returns the queue registered under name, if any.
func (q *queues) lookup(name string) (Queue, bool) {
	q.RLock()
	defer q.RUnlock()
	val, ok := q.registry[name]
	return val, ok
}
