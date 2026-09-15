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

// asyncTransport sends log entries through a bounded worker queue.
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

// newAsync starts the drain worker and applies defaults.
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
			// Drain queued entries before exit.
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

// SendToLogger enqueues without blocking.
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

// GetLoggerInstance returns the inner logger.
func (t *asyncTransport) GetLoggerInstance() any {
	return t.inner.GetLoggerInstance()
}

// Flush waits until accepted entries reach the inner transport.
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

// Close drains the queue, stops the worker, and closes the inner transport.
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

// Dropped returns the number of discarded entries.
func (t *asyncTransport) Dropped() uint64 {
	return t.dropped.Load()
}
