package antree

import (
	"context"
	"fmt"
	"sync"
	"time"

	jsonv2 "encoding/json/v2"
)

type (
	// Queue represents a queue containing tasks to be executed.
	Queue interface {
		// Config returns the queue configuration.
		Config() *QueueConfig

		// Process executes a task payload.
		Process(ctx context.Context, payload []byte) error
	}

	// QueueConfig holds the configuration options for a queue.
	QueueConfig struct {
		// Name is the queue name and must be unique.
		Name string

		// MaxAttempts is the maximum number of execution attempts before the task
		// is marked as completed.
		MaxAttempts int

		// Timeout is the duration set on the context while executing a task.
		Timeout time.Duration

		// Backoff is the duration a failed task waits in the queue before retry.
		Backoff time.Duration

		// Retention dictates if and how completed tasks are retained in the
		// database. If nil, no completed tasks are retained.
		Retention *Retention
	}

	// Retention is the policy for retaining completed tasks in the database.
	Retention struct {
		// Duration is how long a completed task is retained. If omitted, the task
		// is retained forever.
		Duration time.Duration

		// OnlyFailed retains only failed tasks.
		OnlyFailed bool

		// Data provides options for retaining task payload data. If nil, no task
		// payload data is retained.
		Data *RetainData
	}

	// RetainData is the policy for retaining task payload data of completed tasks.
	RetainData struct {
		// OnlyFailed retains payload data only for failed tasks.
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

// NewQueue creates a new type-safe Queue for the given task type.
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

// add adds a queue to the registry, panicking when the name is missing or
// already registered.
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

// lookup loads a queue from the registry by name; false is returned when it
// was never registered.
func (q *queues) lookup(name string) (Queue, bool) {
	q.RLock()
	defer q.RUnlock()
	val, ok := q.registry[name]
	return val, ok
}
