package logger

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"go.loglayer.dev/v3"
	"go.loglayer.dev/v3/transport"
)

// asyncTransport decouples emission from I/O: entries land in a
// bounded queue drained by a single worker goroutine that forwards
// to the inner transport. When the queue is full, entries are
// dropped and counted — emission never blocks, even if the sink
// wedges.
type asyncTransport struct {
	transport.BaseTransport
	inner loglayer.Transport

	entries chan loglayer.TransportParams
	stop    chan struct{}
	done    chan struct{}

	sent      atomic.Uint64
	processed atomic.Uint64

	closeOnce   sync.Once
	innerCloser sync.Once
	closed      atomic.Bool
	dropped     atomic.Uint64

	flushTimeout time.Duration
}

// newAsync starts a worker draining the inner transport. Buffer
// below one uses DefaultBufferSize.
func newAsync(inner loglayer.Transport, buffer int, flushTimeout time.Duration) *asyncTransport {
	if buffer <= 0 {
		buffer = DefaultBufferSize
	}
	if flushTimeout <= 0 {
		flushTimeout = DefaultFlushTimeout
	}
	t := &asyncTransport{
		BaseTransport: transport.NewBaseTransport(transport.BaseConfig{ID: "async"}),
		inner:         inner,
		entries:       make(chan loglayer.TransportParams, buffer),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		flushTimeout:  flushTimeout,
	}
	go t.work()
	return t
}

func (t *asyncTransport) work() {
	defer close(t.done)
	for {
		select {
		case <-t.stop:
			// Draining: forward everything already queued, then exit.
			for {
				select {
				case p := <-t.entries:
					t.inner.SendToLogger(p)
					t.processed.Add(1)
				default:
					return
				}
			}
		case p := <-t.entries:
			t.inner.SendToLogger(p)
			t.processed.Add(1)
		}
	}
}

// SendToLogger enqueues the entry without blocking.
func (t *asyncTransport) SendToLogger(p loglayer.TransportParams) {
	if t.closed.Load() {
		t.dropped.Add(1)
		return
	}
	select {
	case t.entries <- p:
		t.sent.Add(1)
	default:
		t.dropped.Add(1)
	}
}

// GetLoggerInstance exposes the inner transport's underlying logger.
func (t *asyncTransport) GetLoggerInstance() any {
	return t.inner.GetLoggerInstance()
}

// Flush waits, bounded by the timeout, until every accepted entry
// has been handed to the inner transport. It does not close
// anything.
func (t *asyncTransport) Flush(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if t.processed.Load() >= t.sent.Load() {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("logger: flush timed out after %s", timeout)
		}
		time.Sleep(100 * time.Microsecond)
	}
}

// Close stops the worker after draining the queue, then closes the
// inner transport when it is an io.Closer. Idempotent.
func (t *asyncTransport) Close() error {
	t.closed.Store(true)
	t.closeOnce.Do(func() { close(t.stop) })

	select {
	case <-t.done:
	case <-time.After(t.flushTimeout):
		return fmt.Errorf("logger: flush timed out after %s", t.flushTimeout)
	}

	var err error
	t.innerCloser.Do(func() {
		if c, ok := t.inner.(io.Closer); ok {
			err = c.Close()
		}
	})
	return err
}

// Dropped reports how many entries were dropped due to a full queue
// or a post-close emission.
func (t *asyncTransport) Dropped() uint64 {
	return t.dropped.Load()
}
