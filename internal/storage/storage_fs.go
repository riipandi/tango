package storage

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// chunkHashPattern is the shape a chunk filename must have before it is
// believed to be a hash: 64 lowercase hex characters. The chunks directory
// is a scan target, and a stray file — an editor's, a crash's — must not
// become a garbage-collection candidate.
var chunkHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// FS is the local-filesystem backend: every chunk is one file under the
// data directory, spread across two-hex-character directories so a single
// directory never holds the whole store.
type FS struct {
	root string
}

// NewFS builds the local backend over the data directory. The directory is
// not created here: the first write creates it, so a read-only run of a
// command that never stores a file touches nothing.
func NewFS(root string) *FS {
	return &FS{root: root}
}

// HasChunk reports whether the chunk's file exists.
func (s *FS) HasChunk(_ context.Context, hash string) (bool, error) {
	_, err := os.Stat(s.chunkPath(hash))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("storage: stat chunk %s: %w", hash, err)
	}
	return true, nil
}

// PutChunk writes the chunk through a temp file and a rename, so a reader
// never observes half of it and a crash leaves a temp file — the garbage
// collection's scan refuses names that are not hashes — instead of a
// truncated chunk.
func (s *FS) PutChunk(_ context.Context, hash string, data []byte) error {
	path := s.chunkPath(hash)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("storage: chunk directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+hash+".*")
	if err != nil {
		return fmt.Errorf("storage: write chunk %s: %w", hash, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("storage: write chunk %s: %w", hash, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("storage: write chunk %s: %w", hash, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("storage: write chunk %s: %w", hash, err)
	}
	return nil
}

// GetChunk reads the chunk's bytes into dst.
func (s *FS) GetChunk(_ context.Context, dst []byte, hash string) ([]byte, error) {
	data, err := os.ReadFile(s.chunkPath(hash))
	if errors.Is(err, fs.ErrNotExist) {
		return dst, fmt.Errorf("storage: chunk %s: %w", hash, ErrNotFound)
	}
	if err != nil {
		return dst, fmt.Errorf("storage: read chunk %s: %w", hash, err)
	}
	return append(dst, data...), nil
}

// DeleteChunk removes the chunk's file. A chunk already gone is the state
// the caller asked for, not an error.
func (s *FS) DeleteChunk(_ context.Context, hash string) error {
	err := os.Remove(s.chunkPath(hash))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("storage: delete chunk %s: %w", hash, err)
	}
	return nil
}

// ListChunks walks the chunk directories and calls fn for every chunk whose
// name is a hash. Names that are not — temp files, strays — are skipped:
// only the backend's real chunks are listed, and only they may be deleted.
func (s *FS) ListChunks(_ context.Context, fn func(hash string) error) error {
	root := filepath.Join(s.root, "chunks")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("storage: chunk directory: %w", err)
	}
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !chunkHashPattern.MatchString(entry.Name()) {
			return nil
		}
		return fn(entry.Name())
	})
}

// chunkPath is where one chunk lives: chunks/<2 hex>/<hash>.
func (s *FS) chunkPath(hash string) string {
	return filepath.Join(s.root, "chunks", hash[:2], hash)
}
