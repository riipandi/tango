// Package storage holds the file storage engine and its two backends: the
// local filesystem and an S3-compatible object store.
//
// A file is never stored whole. It is cut into fixed-size chunks, each hashed
// with SHA-256, and the hash is the chunk's address in the backend — so two
// files sharing a chunk share one copy, and a changed file re-uploads only
// the chunks that changed. The chunk list of a file (its manifest) lives in
// Postgres, the durable place a diff is read from; the bytes live in the
// backend. The upload itself runs on the durable queue, so a process death
// in the middle loses nothing: the manifest is committed in one transaction
// only after every chunk is in place.
package storage

import (
	"context"
	"errors"

	"github.com/riipandi/tango/internal/config"
)

// ErrNotFound is what a read of an unknown key answers with.
var ErrNotFound = errors.New("storage: not found")

// Store is the chunk surface a backend answers. A chunk is addressed by the
// lowercase hex SHA-256 of its bytes, so PutChunk with a hash a file already
// stores is idempotent — the backend either holds the bytes or gains them,
// and two callers writing the same hash write the same bytes.
//
// Every method carries the caller's context: both backends can be a network
// away, and a cancelled request must not pay for one.
type Store interface {
	// HasChunk reports whether the backend holds the chunk.
	HasChunk(ctx context.Context, hash string) (bool, error)
	// PutChunk stores the chunk bytes under their hash. Writing the same
	// hash twice writes the same bytes twice, never a corruption.
	PutChunk(ctx context.Context, hash string, data []byte) error
	// GetChunk reads the chunk named by hash into dst. The returned slice
	// is dst grown by append, the style the cache driver reads through.
	GetChunk(ctx context.Context, dst []byte, hash string) ([]byte, error)
	// DeleteChunk removes the chunk, if present. A hash no caller
	// references anymore is garbage; CollectGarbage names them.
	DeleteChunk(ctx context.Context, hash string) error
	// ListChunks calls fn for every chunk the backend holds. It is the
	// scan the garbage collection walks; fn may return an error to stop
	// the scan.
	ListChunks(ctx context.Context, fn func(hash string) error) error
}

// New hands back the driver the configuration names. The composition root
// calls it; a driver the configuration selects is the only one constructed,
// so a local deployment never opens an object-store client.
func New(cfg config.Config) (Store, error) {
	switch cfg.Storage.Driver {
	case config.StorageS3:
		return NewS3(cfg.Storage.S3)
	default:
		return NewFS(cfg.Storage.LocalPath), nil
	}
}

// ChunkName lays out the chunks the two backends share: two hex characters
// of the hash spread the files across directories, the rest of the hash
// names the object. The layout is the contract the garbage collection scans.
func ChunkName(hash string) string {
	return "chunks/" + hash[:2] + "/" + hash
}
