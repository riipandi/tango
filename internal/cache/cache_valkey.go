package cache

import (
	"context"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/riipandi/tango/internal/datastore"
)

// ValkeyKeyPrefix namespaces the keys the cache owns inside the shared
// backend. Features sharing one server are isolated by prefix, not by
// logical database index — which Valkey cluster does not support — so a
// prefix is also what an operator scans or cleans per feature.
const ValkeyKeyPrefix = "tango:cache:"

// Valkey is the Cache driver over the shared key-value backend. It holds no
// connection of its own: the backend client is the process-wide one, and the
// driver only names its keys within a prefix so other features can share the
// same server.
type Valkey struct {
	client valkeyClient
	prefix string
	ttl    time.Duration
}

// valkeyClient is the part of the backend client the driver needs. It keeps
// a test free of a real server by handing in a mock of the same shape.
type valkeyClient interface {
	Do(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult
	B() valkey.Builder
}

// NewValkey builds the driver over the shared backend client. defaultTTL is
// the lifetime of an entry that Set stores without an explicit one.
func NewValkey(kv *datastore.Valkey, defaultTTL time.Duration) *Valkey {
	if defaultTTL <= 0 {
		defaultTTL = 5 * time.Minute
	}
	return &Valkey{client: kv.Client(), prefix: ValkeyKeyPrefix, ttl: defaultTTL}
}

// Get implements Cache. A backend that is unreachable, a key that is absent,
// and an error of any kind are all a miss: a cache may degrade, the feature
// behind it may not.
func (c *Valkey) Get(ctx context.Context, dst []byte, key string) ([]byte, bool) {
	value, err := c.client.Do(ctx, c.client.B().Get().Key(c.prefix+key).Build()).AsBytes()
	if err != nil {
		return dst, false
	}
	return append(dst, value...), true
}

// Set implements Cache, with the ttl carried as PX — the per-entry lifetime
// the contract promises, on the server's own clock.
func (c *Valkey) Set(ctx context.Context, key string, value []byte, ttl time.Duration) {
	if ttl <= 0 {
		ttl = c.ttl
	}
	// BinaryString hands the bytes to the command without a copy: the
	// builder owns them for the duration of the request.
	c.client.Do(ctx, c.client.B().Set().
		Key(c.prefix+key).
		Value(valkey.BinaryString(value)).
		Px(ttl).
		Build())
}

// Del implements Cache.
func (c *Valkey) Del(ctx context.Context, key string) {
	c.client.Do(ctx, c.client.B().Del().Key(c.prefix+key).Build())
}
