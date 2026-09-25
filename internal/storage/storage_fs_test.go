package storage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFSStore builds a local backend over a throwaway directory and returns
// a key with known bytes, the shape every store test runs through.
func newFSStore(t *testing.T) (*FS, string, []byte) {
	t.Helper()

	root := t.TempDir()
	data := []byte("a whole file, stored once under the key its feature composed")
	return NewFS(root), "avatars/usr_1/profile-picture", data
}

func TestFSStoreRoundTripsAFile(t *testing.T) {
	store, key, data := newFSStore(t)
	ctx := t.Context()

	keys, err := listedKeys(ctx, store)
	require.NoError(t, err)
	assert.Empty(t, keys)

	require.NoError(t, store.Put(ctx, key, bytes.NewReader(data), int64(len(data))))

	reader, err := store.Get(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, data, got)

	keys, err = listedKeys(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, []string{key}, keys)

	require.NoError(t, store.Delete(ctx, key))
	keys, err = listedKeys(ctx, store)
	require.NoError(t, err)
	assert.Empty(t, keys)
}

func TestFSStorePutReplacesWhole(t *testing.T) {
	store, key, data := newFSStore(t)
	ctx := t.Context()

	require.NoError(t, store.Put(ctx, key, bytes.NewReader(data), int64(len(data))))
	changed := append(bytes.Clone(data), []byte("-changed")...)
	require.NoError(t, store.Put(ctx, key, bytes.NewReader(changed), int64(len(changed))))

	reader, err := store.Get(ctx, key)
	require.NoError(t, err)
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	assert.Equal(t, changed, got)
}

func TestFSStoreGetMissingFileIsNotFound(t *testing.T) {
	store, key, _ := newFSStore(t)

	_, err := store.Get(t.Context(), key)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestFSStoreDeleteMissingFileIsQuiet(t *testing.T) {
	store, key, _ := newFSStore(t)

	assert.NoError(t, store.Delete(t.Context(), key))
}

func TestFSStoreListSkipsTempFiles(t *testing.T) {
	store, key, data := newFSStore(t)
	ctx := t.Context()

	require.NoError(t, store.Put(ctx, key, bytes.NewReader(data), int64(len(data))))
	// A temp file shares the file's directory: a crash's leftover, which
	// the scan must not name, because only real files may be deleted.
	require.NoError(t, os.WriteFile(
		filepath.Join(store.root, filesDir, "avatars", "usr_1", ".profile-picture.tmp"),
		[]byte("x"), 0o600))

	keys, err := listedKeys(ctx, store)
	require.NoError(t, err)
	assert.Equal(t, []string{key}, keys)
}

func TestFSStoreDeletePrunesTheEmptyDirs(t *testing.T) {
	store, key, data := newFSStore(t)
	ctx := t.Context()

	require.NoError(t, store.Put(ctx, key, bytes.NewReader(data), int64(len(data))))
	require.NoError(t, store.Delete(ctx, key))

	_, err := os.Stat(filepath.Join(store.root, filesDir, "avatars", "usr_1"))
	assert.True(t, os.IsNotExist(err), "an emptied subtree must not linger")
}

func TestContentHashIsTheWholeFile(t *testing.T) {
	// The content hash is the engine's "the backend holds these bytes"
	// check; it is the SHA-256 of the whole file, the value the sync
	// commits and the retry compares.
	data := []byte("hash me whole")
	sum := sha256.Sum256(data)
	assert.Equal(t, hex.EncodeToString(sum[:]), hashOf(data))
}
