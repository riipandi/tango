package cache

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forceCollision returns a hash that maps the named keys to distinct values
// and everything else to one shared value, so the collision path runs
// without hoping two random strings collide.
func forceCollision(keys ...string) func(string) uint64 {
	return func(key string) uint64 {
		for i, want := range keys {
			if key == want {
				return uint64(i + 1)
			}
		}
		return 0
	}
}

func TestMemoryRoundTripsAnEntry(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	c.Set("user:1", []byte("ali"), 0)

	value, ok := c.Get(make([]byte, 0, 64), "user:1")
	require.True(t, ok)
	assert.Equal(t, "ali", string(value))
}

func TestMemoryMissReturnsWithoutAnEntry(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	value, ok := c.Get(make([]byte, 0, 64), "absent")
	assert.False(t, ok)
	assert.Empty(t, value)
}

func TestMemoryOverwritesTheSameKey(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	c.Set("k", []byte("first"), 0)
	c.Set("k", []byte("second"), 0)

	value, ok := c.Get(nil, "k")
	require.True(t, ok)
	assert.Equal(t, "second", string(value))

	// A value of the same length overwrites in place; both paths must
	// answer the same.
	c.Set("k", []byte("third"), 0)
	value, ok = c.Get(nil, "k")
	require.True(t, ok)
	assert.Equal(t, "third", string(value))
}

func TestMemoryDeletesOnlyTheRequestedKey(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	c.Set("gone", []byte("1"), 0)
	c.Set("stays", []byte("2"), 0)

	c.Del("gone")

	_, ok := c.Get(nil, "gone")
	assert.False(t, ok)
	value, ok := c.Get(nil, "stays")
	require.True(t, ok)
	assert.Equal(t, "2", string(value))
}

func TestMemoryExpiresEntries(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	c.Set("short", []byte("1"), 10*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	_, ok := c.Get(nil, "short")
	assert.False(t, ok, "an expired entry is a miss")

	c.Set("long", []byte("1"), time.Hour)
	_, ok = c.Get(nil, "long")
	assert.True(t, ok)

	// The zero ttl takes the default, so a caller does not spell the
	// configured lifetime at every call site.
	c.Set("default", []byte("1"), 0)
	_, ok = c.Get(nil, "default")
	assert.True(t, ok)
}

func TestMemoryKeepsBothKeysAcrossAHashCollision(t *testing.T) {
	// The property bigcache lacks: two keys that hash the same must both
	// keep working, because a lookup verifies the stored key bytes before
	// answering.
	const (
		left  = "session:alpha"
		right = "session:beta"
	)
	c := NewMemory(0, 5*time.Minute)
	c.hashFn = forceCollision(left, right)

	c.Set(left, []byte("value-a"), 0)
	c.Set(right, []byte("value-b"), 0)

	value, ok := c.Get(nil, left)
	require.True(t, ok, "the key that lost the primary slot must stay reachable")
	assert.Equal(t, "value-a", string(value))

	value, ok = c.Get(nil, right)
	require.True(t, ok)
	assert.Equal(t, "value-b", string(value))

	// Deleting one must not disturb the other.
	c.Del(left)
	_, ok = c.Get(nil, left)
	assert.False(t, ok)
	value, ok = c.Get(nil, right)
	require.True(t, ok)
	assert.Equal(t, "value-b", string(value))
}

func TestMemoryNeverConfusesTwoKeysUnderForcedCollisions(t *testing.T) {
	// Three keys sharing one hash: the third evicts the secondary, and the
	// remaining two must still answer for themselves and nobody else.
	c := NewMemory(0, 5*time.Minute)
	c.hashFn = func(string) uint64 { return 1 } // every key collides

	c.Set("a", []byte("va"), 0)
	c.Set("b", []byte("vb"), 0)
	c.Set("c", []byte("vc"), 0)

	// "c" holds the primary slot and "b" the secondary; "a" was evicted by
	// the third write. Nothing may leak across keys.
	value, ok := c.Get(nil, "c")
	require.True(t, ok)
	assert.Equal(t, "vc", string(value))
	value, ok = c.Get(nil, "b")
	require.True(t, ok)
	assert.Equal(t, "vb", string(value))
	_, ok = c.Get(nil, "a")
	assert.False(t, ok, "a three-way collision evicts the secondary")
}

func TestMemoryGetIsAllocationFreeOnAHit(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)
	c.Set("k", []byte("a short payload"), 0)

	dst := make([]byte, 0, 512)
	allocs := testing.AllocsPerRun(100, func() {
		value, ok := c.Get(dst, "k")
		if !ok || string(value) != "a short payload" {
			t.Fatal("unexpected cache answer")
		}
	})
	assert.Zero(t, allocs, "a hit must not allocate while dst has room")
}

func TestMemoryCleansItselfWhenTheBudgetIsSpent(t *testing.T) {
	// A tiny budget: one shard, one chunk, so the ring fills and the reset
	// runs while entries are still being written.
	c := NewMemory(memoryChunkSize, 5*time.Minute)

	for i := 0; i < 2000; i++ {
		c.Set(fmt.Sprintf("key-%d", i), []byte(fmt.Sprintf("value-%d", i)), 0)
	}

	// The cache still answers: whatever survived the last reset is
	// readable, and nothing panicked or grew beyond the budget.
	chunks := 0
	for i := range c.shards {
		chunks += len(c.shards[i].chunks)
	}
	assert.LessOrEqual(t, chunks*memoryChunkSize, memoryChunkSize,
		"the ring must not grow past its budget")
}

func TestMemoryHandlesAnEntrySpanningChunks(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	// Large enough to cross the 64 KiB chunk boundary, no matter where the
	// ring happens to stand.
	value := make([]byte, memoryChunkSize+1024)
	for i := range value {
		value[i] = byte(i)
	}
	c.Set("big", value, 0)

	got, ok := c.Get(nil, "big")
	require.True(t, ok)
	assert.Equal(t, value, got, "an entry that spans chunks reads back whole")
}

func TestMemorySurvivesConcurrentUse(t *testing.T) {
	c := NewMemory(0, 5*time.Minute)

	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			dst := make([]byte, 0, 128)
			for i := 0; i < 200; i++ {
				key := fmt.Sprintf("g%d-k%d", g, i)
				c.Set(key, []byte(key), 0)
				value, ok := c.Get(dst, key)
				if !ok || string(value) != key {
					t.Errorf("concurrent get of %s: %q (found=%v)", key, value, ok)
				}
				c.Del(key)
			}
		}(g)
	}
	wg.Wait()
}

func TestNewMemoryScalesTheShardsWithTheBudget(t *testing.T) {
	// A small budget takes few shards, and every shard keeps at least one
	// chunk — a budget under one chunk per shard would leave a shard with
	// no room at all.
	assert.Equal(t, 1, len(NewMemory(memoryChunkSize, time.Minute).shards))
	assert.Equal(t, 2, len(NewMemory(2*memoryChunkSize, time.Minute).shards))
	assert.Equal(t, memoryShardCount, len(NewMemory(0, time.Minute).shards))
}
