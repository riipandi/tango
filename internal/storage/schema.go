// Package storage abstracts blob storage: files on disk in
// development, S3 in production. Implementations are
// file-per-backend; the registry selects one and injects it into
// modules through registry.Deps. Keys are path-like strings,
// opaque to callers ("avatars/01/ab/user-01.png"); backends map
// them to their own layout.
//
// Files:
//
//	schema.go     — scope + blob contract (this file)
//	storage_fs.go — filesystem backend (dev)
//	storage_s3.go — S3 backend (prod)
package storage

import (
	"context"
	"errors"
	"io"
)

// ErrNotFound is returned by Open and Delete when a key has no
// stored object.
var ErrNotFound = errors.New("storage: object not found")

// Store is the blob storage contract: write, read, delete.
// Metadata options (content type, cache headers) will be added as
// an optional parameter when a backend needs them, not by
// widening every implementation up front.
type Store interface {
	Put(ctx context.Context, key string, r io.Reader) error
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}
