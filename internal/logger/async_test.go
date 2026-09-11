package logger

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.loglayer.dev/v3"
	"go.loglayer.dev/v3/transport"
)

// recordingTransport counts deliveries and records Closer state.
type recordingTransport struct {
	transport.BaseTransport
	mu     sync.Mutex
	calls  int
	closed bool
}

func (r *recordingTransport) SendToLogger(loglayer.TransportParams) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
}

func (r *recordingTransport) delivered() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

func (r *recordingTransport) GetLoggerInstance() any { return nil }

func (r *recordingTransport) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// blockingTransport parks the worker inside SendToLogger on the
// first call until released, letting tests fill the queue
// deterministically.
type blockingTransport struct {
	transport.BaseTransport
	entered chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingTransport) SendToLogger(loglayer.TransportParams) {
	if b.calls.Add(1) == 1 {
		b.entered <- struct{}{}
		<-b.release
	}
}

func (b *blockingTransport) GetLoggerInstance() any { return nil }

func TestAsyncDeliversAll(t *testing.T) {
	inner := &recordingTransport{}
	tr := newAsync(inner, 100, time.Second)

	for i := 0; i < 50; i++ {
		tr.SendToLogger(loglayer.TransportParams{})
	}
	require.NoError(t, tr.Close())

	assert.Equal(t, 50, inner.delivered())
	assert.True(t, inner.closed, "inner closer must run")
}

func TestAsyncDropsWhenFull(t *testing.T) {
	inner := &blockingTransport{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	tr := newAsync(inner, 1, time.Second)

	// Worker parks inside the inner transport; entry A is in flight.
	tr.SendToLogger(loglayer.TransportParams{})
	<-inner.entered

	// B fills the single buffer slot, C has nowhere to go.
	tr.SendToLogger(loglayer.TransportParams{})
	tr.SendToLogger(loglayer.TransportParams{})
	assert.Equal(t, uint64(1), tr.Dropped())

	// Release the worker, then drain: A and B must both arrive.
	inner.release <- struct{}{}
	require.NoError(t, tr.Close())

	assert.Equal(t, int32(2), inner.calls.Load())
}

func TestAsyncSendAfterCloseDrops(t *testing.T) {
	inner := &recordingTransport{}
	tr := newAsync(inner, 8, time.Second)
	require.NoError(t, tr.Close())

	tr.SendToLogger(loglayer.TransportParams{})
	assert.Equal(t, uint64(1), tr.Dropped())
	assert.Equal(t, 0, inner.delivered())
}

func TestAsyncCloseIdempotent(t *testing.T) {
	inner := &recordingTransport{}
	tr := newAsync(inner, 8, time.Second)

	require.NoError(t, tr.Close())
	require.NoError(t, tr.Close())
	assert.True(t, inner.closed)
}

func TestAsyncFlush(t *testing.T) {
	inner := &recordingTransport{}
	tr := newAsync(inner, 100, time.Second)

	for i := 0; i < 10; i++ {
		tr.SendToLogger(loglayer.TransportParams{})
	}
	require.NoError(t, tr.Flush(2*time.Second))

	assert.Equal(t, 10, inner.delivered())
	require.NoError(t, tr.Close())
}

func TestAsyncFlushTimeout(t *testing.T) {
	inner := &blockingTransport{
		entered: make(chan struct{}, 1),
		release: make(chan struct{}),
	}
	tr := newAsync(inner, 1, time.Second)

	tr.SendToLogger(loglayer.TransportParams{})
	<-inner.entered // worker parked; queue drains nothing

	assert.Error(t, tr.Flush(50*time.Millisecond))

	inner.release <- struct{}{}
	require.NoError(t, tr.Close())
}
