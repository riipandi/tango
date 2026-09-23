package queue

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestTheWakeupSendsNeverBlock pins the non-blocking property of the sends the
// dispatcher makes to itself. Each one runs on a goroutine that must not stall:
// a worker's token send is its last act before the deferred Done a stop waits
// on, and a wakeup sent by the fetcher arms the fetch that follows it.
//
// A full buffer is the state each has to tolerate, not an error. The token
// channel is full exactly when every worker is already counted free — the token
// being returned is then redundant — and the ready buffer is full when fetches
// are already pending, so a dropped signal costs nothing the fallback clock does
// not cover.
func TestTheWakeupSendsNeverBlock(t *testing.T) {
	d := &dispatcher{
		availableWorkers: make(chan struct{}, 1),
		ready:            make(chan struct{}, 1),
	}
	// Fill both buffers, which is the state that made the send block.
	d.availableWorkers <- struct{}{}
	d.ready <- struct{}{}

	sends := map[string]func(){
		"releaseWorker": d.releaseWorker,
		"signalReady":   func() { _ = d.signalReady() },
	}
	for name, send := range sends {
		t.Run(name, func(t *testing.T) {
			done := make(chan struct{})
			go func() {
				send()
				close(done)
			}()

			select {
			case <-done:
			case <-time.After(time.Second):
				require.Fail(t, "the send must not block on a full buffer")
			}
		})
	}
}

// TestAFullReadyBufferStillArmsAFetch is the other half: a wakeup that could not
// be delivered must leave a fetch armed, or a task already due would wait out
// the fallback clock with nothing to wake it.
//
// The fallback is shortened so the armed fetch can be observed. The first
// subtest proves the arming happens; the second proves the test would notice if
// it did not — without the arming the ticker stays stopped, which is the state
// that leaves the task waiting on nothing.
func TestAFullReadyBufferStillArmsAFetch(t *testing.T) {
	newDispatcher := func() *dispatcher {
		d := &dispatcher{
			availableWorkers: make(chan struct{}, 1),
			ready:            make(chan struct{}, 1),
			fallbackPoll:     20 * time.Millisecond,
		}
		d.ready <- struct{}{} // full, so the signal below cannot be delivered
		d.ticker = time.NewTicker(time.Hour)
		d.ticker.Stop()
		return d
	}

	t.Run("the wakeup arms the fallback", func(t *testing.T) {
		d := newDispatcher()
		defer d.ticker.Stop()

		d.fetchNow()

		select {
		case <-d.ticker.C:
			// The armed fallback fired, so the fetch is bounded even though
			// the signal was dropped.
		case <-time.After(2 * time.Second):
			require.Fail(t, "the dropped wakeup must leave a fetch armed")
		}
	})

	t.Run("an unarmed ticker never fires", func(t *testing.T) {
		// The same dispatcher with nothing armed: this is what the test above
		// would see if fetchNow dropped the wakeup without arming anything, so
		// the assertion is not satisfied by a ticker that fires on its own.
		d := newDispatcher()
		defer d.ticker.Stop()

		select {
		case <-d.ticker.C:
			require.Fail(t, "a stopped ticker must not fire")
		case <-time.After(100 * time.Millisecond):
		}
	})
}
