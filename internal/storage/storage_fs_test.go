package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFSStore builds a local backend over a throwaway directory and returns
// a chunk hash with known bytes, the shape every store test runs through.
func newFSStore(t *testing.T) (*FS, string, []byte) {
	t.Helper()

	root := t.TempDir()
	data := []byte("a chunk of bytes, stored once and named by its hash")
	hash := sha256.Sum256(data)
	return NewFS(root), hex.EncodeToString(hash[:]), data
}

// hasChunks answers the batch probe with one hash, the shape every store
// test runs through.
func hasChunks(store Store, ctx context.Context, hash string) (bool, error) {
	found, err := store.HasChunks(ctx, []string{hash})
	if err != nil {
		return false, err
	}
	return found[hash], nil
}

func TestFSStoreRoundTripsAChunk(t *testing.T) {
	store, hash, data := newFSStore(t)
	ctx := t.Context()

	exists, err := hasChunks(store, ctx, hash)
	require.NoError(t, err)
	assert.False(t, exists)

	require.NoError(t, store.PutChunk(ctx, hash, data))

	exists, err = hasChunks(store, ctx, hash)
	require.NoError(t, err)
	assert.True(t, exists)

	got, err := store.GetChunk(ctx, nil, hash)
	require.NoError(t, err)
	assert.Equal(t, data, got)

	require.NoError(t, store.DeleteChunk(ctx, hash))
	exists, err = hasChunks(store, ctx, hash)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestFSStorePutChunkIsIdempotent(t *testing.T) {
	store, hash, data := newFSStore(t)
	ctx := t.Context()

	require.NoError(t, store.PutChunk(ctx, hash, data))
	require.NoError(t, store.PutChunk(ctx, hash, data))

	got, err := store.GetChunk(ctx, nil, hash)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestFSStoreGetMissingChunkIsNotFound(t *testing.T) {
	store, hash, _ := newFSStore(t)

	_, err := store.GetChunk(t.Context(), nil, hash)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFSStoreDeleteMissingChunkIsQuiet(t *testing.T) {
	store, hash, _ := newFSStore(t)

	assert.NoError(t, store.DeleteChunk(t.Context(), hash))
}

func TestFSStoreListChunksYieldsOnlyHashNames(t *testing.T) {
	store, hash, data := newFSStore(t)
	ctx := t.Context()

	require.NoError(t, store.PutChunk(ctx, hash, data))
	// A stray and a temp file share the chunk directories: the scan must
	// name neither, because only real chunks may be deleted.
	require.NoError(t, os.WriteFile(filepath.Join(store.root, "chunks", hash[:2], "notes.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(store.root, "chunks", hash[:2], "."+hash+".tmp"), []byte("x"), 0o600))

	var listed []string
	require.NoError(t, store.ListChunks(ctx, func(h string) error {
		listed = append(listed, h)
		return nil
	}))
	assert.Equal(t, []string{hash}, listed)
}

func TestChunkNameLayoutIsSharedByTheBackends(t *testing.T) {
	// One layout, two backends: the garbage collection's scan and the
	// drivers' addressing must never disagree about where a chunk lives.
	assert.Equal(t, "chunks/ab/abcd", ChunkName("abcd"))
}
