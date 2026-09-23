package queue

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drainProbeTask runs long enough to be in flight when a drain begins, the
// way a chunk upload does under its own multi-minute timeout.
type drainProbeTask struct {
	Name string `json:"name"`
}

func (drainProbeTask) Config() QueueConfig {
	return QueueConfig{
		Name:        "drain_probe",
		MaxAttempts: 1,
		Timeout:     30 * time.Minute,
	}
}

// TestASignalDoesNotCancelAnInFlightTask is the regression this pins: the
// start context is the run's signal context, so cancelling it is exactly what
// SIGTERM does. A task in flight must keep a live context and finish, rather
// than be abandoned mid-execution and left for its release window.
func TestASignalDoesNotCancelAnInFlightTask(t *testing.T) {
	client, err := NewClient(ClientConfig{
		Store:        migratedPool(t),
		Logger:       slog.Default(),
		NumWorkers:   1,
		ReleaseAfter: 60 * time.Second,
	})
	require.NoError(t, err)

	signalCtx, cancelSignal := context.WithCancel(t.Context())
	defer cancelSignal()

	var (
		mu       sync.Mutex
		sawErr   error
		finished bool
	)
	started := make(chan struct{})

	client.Register(NewQueue[drainProbeTask](func(ctx context.Context, _ drainProbeTask) error {
		close(started)
		select {
		case <-ctx.Done():
			mu.Lock()
			sawErr = ctx.Err()
			mu.Unlock()
		case <-time.After(2 * time.Second):
		}
		mu.Lock()
		finished = true
		mu.Unlock()
		return nil
	}))
	client.Start(signalCtx)

	save(t, client.Add(drainProbeTask{Name: "slow"}))

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		require.Fail(t, "the task never started")
	}

	// The signal arrives while the task runs, which is the sequence a SIGTERM
	// produces: the start context is cancelled, then the drain begins.
	cancelSignal()

	// A drain bounded like serve's own window. The task must finish inside it
	// with a live context.
	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(t.Context()), 10*time.Second)
	defer cancelStop()
	assert.True(t, client.Stop(stopCtx), "the task must finish inside the drain window")

	mu.Lock()
	defer mu.Unlock()
	assert.NoError(t, sawErr, "the signal must not reach the task's context")
	assert.True(t, finished, "the task must run to completion")
}

// TestStoppingAfterASignalStillReleasesTheTaskContext covers the other path
// into stop: the start context was cancelled, the dispatcher ended on its
// own, and the stop that follows must still release the task context rather
// than leave it to the process's lifetime.
func TestStoppingAfterASignalStillReleasesTheTaskContext(t *testing.T) {
	client := newTestClient(t)
	client.Register(NewQueue[probeTask](func(context.Context, probeTask) error { return nil }))

	startCtx, cancelStart := context.WithCancel(t.Context())
	client.Start(startCtx)

	// The signal, with no task in flight.
	cancelStart()

	stopCtx, cancelStop := context.WithTimeout(context.WithoutCancel(t.Context()), 5*time.Second)
	defer cancelStop()

	assert.True(t, client.Stop(stopCtx), "a stop after the dispatcher ended is still a success")

	// The dispatcher ended with the signal, and the task context is released:
	// the assertion is that the call did not panic on an already-released
	// context and that a second stop is equally harmless.
	assert.True(t, client.Stop(stopCtx))
}
