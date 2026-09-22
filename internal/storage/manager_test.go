package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// hasChunk answers the batch probe with one hash, the shape the manager
// tests assert through.
func hasChunk(ctx context.Context, store Store, hash string) (bool, error) {
	found, err := store.HasChunks(ctx, []string{hash})
	if err != nil {
		return false, err
	}
	return found[hash], nil
}

// migratedPool applies the migrations to a fresh test database and returns
// the pool the manager's manifest reads and writes go through.
func migratedPool(t *testing.T) *datastore.Postgres {
	t.Helper()

	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	migrationDB, err := datastore.OpenMigrationDB(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	migrator, err := database.NewMigrator(t.Context(), migrationDB, database.MigratorOptions{})
	require.NoError(t, err)
	_, err = migrator.Up(t.Context())
	require.NoError(t, err)
	require.NoError(t, migrationDB.Close())

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{
		DSN:             dsn,
		ApplicationName: "storage_test",
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

// newManager builds the engine over a small chunk size — 32 bytes, so a few
// kilobytes make several chunks — and a throwaway staging directory.
func newManager(t *testing.T) (*Manager, *FS, string) {
	t.Helper()

	pool := migratedPool(t)
	store := NewFS(t.TempDir())
	staging := t.TempDir()
	manager, err := NewManager(store, pool, 32, staging, 2)
	require.NoError(t, err)
	return manager, store, staging
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// chunkListOf cuts data the way a chunker of the given size would, the
// expected manifest a test asserts against.
func chunkListOf(data []byte, size int) []Chunk {
	chunker, err := NewChunker(size)
	if err != nil {
		panic(err)
	}
	chunks, err := chunker.Split(bytes.NewReader(data))
	if err != nil {
		panic(err)
	}
	return chunks
}

func TestManagerSyncStoresTheManifestAndDropsTheStagingFile(t *testing.T) {
	manager, store, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("chunked"), 100) // 700 bytes, 22 chunks
	require.NoError(t, manager.Stage(ctx, "docs/report.txt", bytes.NewReader(data), nil))
	require.NoError(t, manager.Sync(ctx, "docs/report.txt"))

	// The staging file has served its purpose: the bytes are chunk-addressed
	// in the backend and the manifest is committed.
	_, err := os.Stat(manager.stagingPath("docs/report.txt"))
	assert.True(t, errors.Is(err, os.ErrNotExist), "the staging file must be gone after a sync")

	manifest, err := manager.manifests.Load(ctx, manager.db, "docs/report.txt")
	require.NoError(t, err)
	require.Len(t, manifest.Chunks, 22)
	assert.Equal(t, int64(len(data)), manifest.File.Size)
	assert.Equal(t, StatusReady, manifest.File.Status)
	assert.Equal(t, RootHash(chunkListOf(data, 32)), manifest.File.ContentHash)

	// Every chunk the manifest names is in the backend under its hash.
	for _, chunk := range manifest.Chunks {
		exists, err := hasChunk(ctx, store, chunk.Hash)
		require.NoError(t, err)
		assert.True(t, exists, "chunk %d missing from the backend", chunk.Index)
	}

	// A read back assembles the file whole; the caller never sees the
	// chunking.
	reader, err := manager.Open(ctx, "docs/report.txt")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestManagerSyncOfTheSameBytesIsAShortCircuit(t *testing.T) {
	manager, _, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("stable"), 50)
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))
	require.NoError(t, manager.Sync(ctx, "k"))

	// The second sync reads the same bytes: the root hash matches, so no
	// chunk is re-uploaded and the answer is still a stored file.
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))
	require.NoError(t, manager.Sync(ctx, "k"))

	reader, err := manager.Open(ctx, "k")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestManagerReuploadTouchesOnlyTheChangedChunk(t *testing.T) {
	manager, store, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("A"), 96) // three chunks of 32
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))
	require.NoError(t, manager.Sync(ctx, "k"))

	changed := bytes.Clone(data)
	changed[40] = 'B' // inside the second chunk only
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(changed), nil))
	require.NoError(t, manager.Sync(ctx, "k"))

	// The untouched chunks keep their address: a one-byte edit near the
	// middle re-uploads one chunk of a three-chunk file.
	manifest, err := manager.manifests.Load(ctx, manager.db, "k")
	require.NoError(t, err)
	require.Len(t, manifest.Chunks, 3)
	assert.Equal(t, hashOf(data[:32]), manifest.Chunks[0].Hash)
	assert.NotEqual(t, hashOf(data[32:64]), manifest.Chunks[1].Hash)
	assert.Equal(t, hashOf(data[64:]), manifest.Chunks[2].Hash)

	// The superseded chunk is still in the backend: content addressing
	// never deletes on upload — the garbage collection does, once nothing
	// references the old bytes.
	oldHash := hashOf(data[32:64])
	exists, err := hasChunk(ctx, store, oldHash)
	require.NoError(t, err)
	assert.True(t, exists, "the replaced chunk waits for the garbage collection")

	// The read answers with the newest version.
	reader, err := manager.Open(ctx, "k")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, changed, got)
}

func TestManagerDeleteKeepsAChunkAnotherFileShares(t *testing.T) {
	manager, store, _ := newManager(t)
	ctx := t.Context()

	shared := bytes.Repeat([]byte("shared"), 32)
	unique := bytes.Repeat([]byte("unique"), 32)

	// Both files carry the same first 32 bytes: one chunk object serves
	// them both.
	require.NoError(t, manager.Stage(ctx, "left", bytes.NewReader(append(append([]byte{}, shared[:32]...), unique...)), nil))
	require.NoError(t, manager.Sync(ctx, "left"))
	require.NoError(t, manager.Stage(ctx, "right", bytes.NewReader(append(append([]byte{}, shared[:32]...), unique...)), nil))
	require.NoError(t, manager.Sync(ctx, "right"))

	require.NoError(t, manager.Delete(ctx, "left"))

	// left's unique chunks are gone, the shared chunk survives for right.
	_, err := manager.Open(ctx, "left")
	assert.ErrorIs(t, err, ErrNotFound)

	reader, err := manager.Open(ctx, "right")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, append(append([]byte{}, shared[:32]...), unique...), got)

	sharedHash := hashOf(shared[:32])
	exists, err := hasChunk(ctx, store, sharedHash)
	require.NoError(t, err)
	assert.True(t, exists, "a chunk another file references is not deleted")
}

func TestManagerCollectGarbageRemovesOnlyUnreferencedChunks(t *testing.T) {
	manager, store, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("gced"), 32)
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))
	require.NoError(t, manager.Sync(ctx, "k"))

	// An upload that died after its PutChunk: the bytes are in the
	// backend, no manifest names them.
	orphan := []byte("orphaned chunk bytes")
	orphanHash := hashOf(orphan)
	require.NoError(t, store.PutChunk(ctx, orphanHash, orphan))

	removed, err := manager.CollectGarbage(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	exists, err := hasChunk(ctx, store, orphanHash)
	require.NoError(t, err)
	assert.False(t, exists)

	// The referenced chunk survives the sweep.
	manifest, err := manager.manifests.Load(ctx, manager.db, "k")
	require.NoError(t, err)
	exists, err = hasChunk(ctx, store, manifest.Chunks[0].Hash)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestManagerSyncWithoutAStagingFileIsQuiet(t *testing.T) {
	manager, _, _ := newManager(t)

	assert.NoError(t, manager.Sync(t.Context(), "never-staged"))
}
