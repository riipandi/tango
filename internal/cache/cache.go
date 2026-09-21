// Package cache holds the key-value cache drivers the application reads
// through. The in-memory driver keeps entries in process memory; the kvstore
// driver keeps them in the optional Valkey backend.
package cache

import (
	"time"
)

// Cache is the key-value surface a feature reads through. Every method is
// safe for concurrent use.
//
// Values are bytes, and keys are strings: a cache entry is a serialized
// payload, so the driver never inspects what it holds. Get writes into dst —
// the append-style of an API designed to be used in zero-allocation mode, so
// a hot call site reuses its buffer and a hit costs no allocation at all.
type Cache interface {
	// Get reads the entry key holds into dst and reports whether it was
	// found and not expired. The returned slice is dst grown by append, so
	// it is valid until the caller reuses dst.
	Get(dst []byte, key string) ([]byte, bool)
	// Set stores value under key until ttl elapses. A ttl of zero or less
	// means the driver's configured default. Set overwrites the value of
	// the same key; it never disturbs an entry under a different key.
	Set(key string, value []byte, ttl time.Duration)
	// Del removes the entry under key, if any.
	Del(key string)
}
