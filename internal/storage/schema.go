// Package storage abstracts blob storage: disk in dev, S3 in
// prod. Registry picks one, injects via registry.Deps. Keys are
// opaque path-like strings ("avatars/01/ab/user-01.png").
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound from Open/Delete on missing keys.
var ErrNotFound = errors.New("storage: object not found")

// Store is write/read/delete. Metadata (content type, cache)
// comes later as an optional param, not by widening all impls.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
