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
	"fmt"
	"strings"

	"github.com/riipandi/tango/internal/config"
)

// ErrNotFound is what a read of an unknown key answers with.
var ErrNotFound = errors.New("storage: not found")

// ErrInvalidKey is what a key that names nothing inside the staging
// directory answers with: an empty key, an absolute path, a ".." segment,
// or a name no sane file system stores.
var ErrInvalidKey = errors.New("storage: invalid key")

// Store is the chunk surface a backend answers. A chunk is addressed by the
// lowercase hex SHA-256 of its bytes, so PutChunk with a hash a file already
// stores is idempotent — the backend either holds the bytes or gains them,
// and two callers writing the same hash write the same bytes.
//
// Every method carries the caller's context: both backends can be a network
// away, and a cancelled request must not pay for one.
type Store interface {
	// HasChunks reports which of the named hashes the backend holds. One
	// answer for the whole batch is what keeps a sync's probing at one
	// round trip per prefix instead of one per chunk.
	HasChunks(ctx context.Context, hashes []string) (map[string]bool, error)
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

// Key builds a storage key from flexible parts, the naming a multi-purpose
// store needs: the first part is the purpose the files belong to — avatar,
// user-files, export — the rest free, so a feature names its files its own
// way without the engine knowing the scheme:
//
//	Key("avatar", userID, "128.png")     → avatar/usr_123/128.png
//	Key("user-files", userID, report)    → user-files/usr_123/q4-report.pdf
//
// Each part becomes one path segment: characters outside letters, digits,
// dot, dash, and underscore become underscores, and a part that is empty or
// "." or ".." — the shapes a traversal or a hidden file is made of — is
// refused with ErrInvalidKey. The caller decides the segments; the engine
// only guarantees every key names a path inside its directories.
func Key(parts ...string) (string, error) {
	if len(parts) == 0 {
		return "", ErrInvalidKey
	}
	segments := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || !hasSafeRune(part) {
			return "", fmt.Errorf("%w: empty or reserved part %q", ErrInvalidKey, part)
		}
		segment := sanitizeSegment(part)
		if segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return "", fmt.Errorf("%w: reserved part %q", ErrInvalidKey, part)
		}
		segments = append(segments, segment)
	}
	return strings.Join(segments, "/"), nil
}

// ValidateKey checks a key the caller composed itself, the same rules Key
// applies per part: no traversal, no hidden or empty segment, no separator
// tricks.
func ValidateKey(key string) error {
	if key == "" || strings.ContainsRune(key, '\\') {
		return fmt.Errorf("%w: %q", ErrInvalidKey, key)
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("%w: %q is absolute", ErrInvalidKey, key)
	}
	for segment := range strings.SplitSeq(key, "/") {
		if segment == "" || segment == "." || segment == ".." || strings.HasPrefix(segment, ".") {
			return fmt.Errorf("%w: %q has segment %q", ErrInvalidKey, key, segment)
		}
		if segment != sanitizeSegment(segment) {
			return fmt.Errorf("%w: %q has unsafe segment %q", ErrInvalidKey, key, segment)
		}
	}
	return nil
}

// sanitizeSegment maps one part onto a safe path segment: letters, digits,
// dot, dash, and underscore stay, everything else — spaces, slashes, unicode
// punctuation — becomes an underscore. A part built of nothing else is the
// caller's error to see, not a silent "unnamed" file.
func sanitizeSegment(part string) string {
	var b strings.Builder
	b.Grow(len(part))
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// hasSafeRune reports whether a part survives sanitizing with anything left
// of itself: a part of only replaced characters names no file.
func hasSafeRune(part string) bool {
	for _, r := range part {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '-', r == '_':
			return true
		}
	}
	return false
}
