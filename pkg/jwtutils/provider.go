package jwtutils

import (
	"context"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
)

// KeyProvider supplies the active signing key (private side) and the
// public verification key set. Implementations own key storage and
// rotation; consumers (token issuance, discovery) only read.
type KeyProvider interface {
	// SignKey returns the current signing key.
	SignKey(ctx context.Context) (jwk.Key, error)
	// VerifyKeySet returns every currently published public key,
	// including keys still valid during a rotation overlap.
	VerifyKeySet(ctx context.Context) (jwk.Set, error)
}

// CachedKeyProvider wraps a KeyProvider with a TTL cache. Rotation
// becomes visible after the TTL or immediately via Invalidate; the
// TTL doubles as the JWKS propagation delay for verifiers.
type CachedKeyProvider struct {
	inner KeyProvider
	ttl   time.Duration
	now   func() time.Time

	mu      sync.RWMutex
	signKey jwk.Key
	keySet  jwk.Set
	loaded  time.Time
}

// NewCachedKeyProvider builds a cache in front of inner with the
// given entry lifetime.
func NewCachedKeyProvider(inner KeyProvider, ttl time.Duration) *CachedKeyProvider {
	return &CachedKeyProvider{inner: inner, ttl: ttl, now: time.Now}
}

// WithClock overrides the time source (tests).
func (c *CachedKeyProvider) WithClock(now func() time.Time) *CachedKeyProvider {
	c.now = now
	return c
}

// SignKey returns the cached signing key, refreshing it when the
// entry is older than the TTL.
func (c *CachedKeyProvider) SignKey(ctx context.Context) (jwk.Key, error) {
	c.mu.RLock()
	fresh := c.loaded.Add(c.ttl).After(c.now()) && c.signKey != nil
	key := c.signKey
	c.mu.RUnlock()
	if fresh {
		return key, nil
	}

	key, err := c.inner.SignKey(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.signKey, c.loaded = key, c.now()
	c.mu.Unlock()
	return key, nil
}

// VerifyKeySet returns the cached public key set under the same TTL.
func (c *CachedKeyProvider) VerifyKeySet(ctx context.Context) (jwk.Set, error) {
	c.mu.RLock()
	fresh := c.loaded.Add(c.ttl).After(c.now()) && c.keySet != nil
	set := c.keySet
	c.mu.RUnlock()
	if fresh {
		return set, nil
	}

	set, err := c.inner.VerifyKeySet(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.keySet, c.loaded = set, c.now()
	c.mu.Unlock()
	return set, nil
}

// Invalidate drops both cached entries; the next read reloads them.
func (c *CachedKeyProvider) Invalidate() {
	c.mu.Lock()
	c.signKey, c.keySet, c.loaded = nil, nil, time.Time{}
	c.mu.Unlock()
}
