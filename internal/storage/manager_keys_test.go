package storage

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/datastore"
)

// openStaging opens a staging file the way Sync does, the handle the test
// hashes through.
func openStaging(m *Manager, key string) (*os.File, error) {
	return os.Open(m.stagingPath(key))
}

// closeStaging closes a handle the test opened.
func closeStaging(f *os.File) { _ = f.Close() }

// stagingMtime reads the fingerprint half the checkpoint stores.
func stagingMtime(m *Manager, key string) time.Time {
	info, err := os.Stat(m.stagingPath(key))
	if err != nil {
		panic(err)
	}
	return info.ModTime()
}

// chunkBytes cuts one chunk out of the test data, the bytes a chunk index
// names.
func chunkBytes(data []byte, size, index int) []byte {
	start := index * size
	end := min(start+size, len(data))
	return data[start:end]
}

func TestKeyComposesFlexibleSegments(t *testing.T) {
	// The store is multi-purpose: the first part names the purpose, the
	// rest is the feature's own naming — the engine only guarantees each
	// part lands as one safe segment.
	key, err := Key("avatar", "usr_123", "128.png")
	require.NoError(t, err)
	assert.Equal(t, "avatar/usr_123/128.png", key)

	key, err = Key("user-files", "usr_123", "Q4 report final.pdf")
	require.NoError(t, err)
	assert.Equal(t, "user-files/usr_123/Q4_report_final.pdf", key,
		"spaces and unsafe characters become underscores, never separators")
}

func TestKeyRefusesTheShapesATraversalIsMadeOf(t *testing.T) {
	_, err := Key("docs", "..", "etc")
	assert.ErrorIs(t, err, ErrInvalidKey)

	_, err = Key("")
	assert.ErrorIs(t, err, ErrInvalidKey)

	_, err = Key("   ")
	assert.ErrorIs(t, err, ErrInvalidKey, "a part the sanitizer empties is an error, not an unnamed file")
}

func TestValidateKeyRejectsTraversalAndHiddenSegments(t *testing.T) {
	for _, key := range []string{
		"../../etc/passwd",
		"/absolute",
		"docs/./report",
		"docs/../report",
		"docs/.hidden",
		"docs//report",
		`docs\report`,
		"",
	} {
		assert.ErrorIs(t, ValidateKey(key), ErrInvalidKey, "key %q must be refused", key)
	}
	for _, key := range []string{"docs/report.txt", "avatar/usr_1/128.png", "single"} {
		assert.NoError(t, ValidateKey(key), "key %q must be accepted", key)
	}
}

func TestManagerRefusesAnInvalidKeyBeforeTouchingAnything(t *testing.T) {
	manager, _, _ := newManager(t)
	ctx := t.Context()

	// The staging path is built by joining the key; a traversal in the key
	// must die at the door, not walk out of the staging directory.
	assert.ErrorIs(t, manager.Stage(ctx, "../escape", bytes.NewReader([]byte("x")), nil), ErrInvalidKey)
	assert.ErrorIs(t, manager.Sync(ctx, "../escape"), ErrInvalidKey)
	assert.ErrorIs(t, manager.Delete(ctx, "../escape"), ErrInvalidKey)
	assert.ErrorIs(t, manager.UpdateMetadata(ctx, "../escape", nil), ErrInvalidKey)

	_, err := manager.Open(ctx, "../escape")
	assert.ErrorIs(t, err, ErrNotFound, "a read of an invalid key finds nothing, not a traversal")
}

func TestManagerCarriesAndRewritesMetadata(t *testing.T) {
	manager, _, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("meta"), 40)
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), map[string]any{
		"content_type": "application/pdf",
		"original":     "q4 report.pdf",
		"owner_id":     "usr_123",
	}))
	require.NoError(t, manager.Sync(ctx, "k"))

	// The metadata survives the upload: it was written at Stage time and
	// the manifest commits carried it through.
	manifest, err := manager.Manifest(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, "application/pdf", manifest.File.Metadata["content_type"])
	assert.Equal(t, "q4 report.pdf", manifest.File.Metadata["original"])

	// A rewrite touches only the metadata; the manifest and the chunks are
	// exactly as they were.
	require.NoError(t, manager.UpdateMetadata(ctx, "k", map[string]any{
		"content_type": "application/pdf",
		"original":     "renamed.pdf",
	}))
	after, err := manager.Manifest(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, manifest.File.ContentHash, after.File.ContentHash)
	assert.Equal(t, manifest.File.ChunkCount, after.File.ChunkCount)
	assert.Equal(t, "renamed.pdf", after.File.Metadata["original"])
	assert.Equal(t, StatusReady, after.File.Status)
}

func TestManagerProgressFollowsTheUpload(t *testing.T) {
	manager, _, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("progress"), 40) // several chunks
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))

	// While the file still waits in staging, the row Stage created reports
	// pending with nothing counted — the status reader's before picture.
	before, err := manager.Progress(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, StatusPending, before.Status)
	assert.Equal(t, 0, before.Done)

	require.NoError(t, manager.Sync(ctx, "k"))

	progress, err := manager.Progress(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, StatusReady, progress.Status)
	assert.Equal(t, progress.Total, progress.Done, "a finished round counts every chunk")
	assert.Greater(t, progress.Total, 1, "the test file must span several chunks")
	assert.Equal(t, int64(len(data)), progress.Size)
}

func TestManagerRetryReusesTheCheckpointedManifest(t *testing.T) {
	// The checkpoint is what makes a retry of a large file cheap: a
	// pending manifest whose staging fingerprint matches is reused whole,
	// and the upload resumes from the chunks the backend still lacks.
	manager, store, _ := newManager(t)
	ctx := t.Context()

	data := bytes.Repeat([]byte("checkpoint"), 40)
	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader(data), nil))

	// The sync "dies" after the checkpoint but before its upload: the
	// manifest is pending, the chunks are missing, the staging file sits.

	// Simulate the interrupted round: hash and checkpoint only.
	f, err := openStaging(manager, "k")
	require.NoError(t, err)
	chunks, err := manager.chunker.Split(f)
	require.NoError(t, err)
	closeStaging(f)
	checkpoint := Manifest{
		File: File{
			Key:          "k",
			ChunkSize:    manager.chunker.size,
			ChunkCount:   len(chunks),
			ContentHash:  RootHash(chunks),
			Status:       StatusPending,
			StagingSize:  int64(len(data)),
			StagingMtime: stagingMtime(manager, "k"),
		},
		Chunks: chunks,
	}
	require.NoError(t, manager.db.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		return manager.manifests.Save(ctx, tx, checkpoint)
	}))

	// Half the chunks land before the crash.
	for _, chunk := range chunks[:len(chunks)/2] {
		require.NoError(t, store.PutChunk(ctx, chunk.Hash, chunkBytes(data, manager.chunker.size, chunk.Index)))
	}

	// The retry finds the fingerprint unchanged: the chunk list is
	// reused, only the missing half uploads, and the file lands ready.
	require.NoError(t, manager.Sync(ctx, "k"))

	progress, err := manager.Progress(ctx, "k")
	require.NoError(t, err)
	assert.Equal(t, StatusReady, progress.Status)
	assert.Equal(t, progress.Total, progress.Done)

	reader, err := manager.Open(ctx, "k")
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	got, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, got)
}

func TestStorageFilesRefuseAnUnknownStatus(t *testing.T) {
	// The CHECK constraint is the database's own word for the status
	// vocabulary; this test keeps it from drifting away in a migration.
	pool := migratedPool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx,
		`INSERT INTO storage_files (key, chunk_size, content_hash, status)
		 VALUES ('k', 32, '', 'archived')`)
	require.Error(t, err, "a status outside the CHECK list must be refused")
	assert.Contains(t, err.Error(), "chk_storage_files_status")
}

func TestStagingMtimeRoundTrips(t *testing.T) {
	// The fingerprint reuse compares what the database stored against what
	// the file system reports, so a round trip through Postgres must not
	// lose the mtime's precision.
	manager, _, _ := newManager(t)
	ctx := t.Context()

	require.NoError(t, manager.Stage(ctx, "k", bytes.NewReader([]byte("x")), nil))
	manifest, err := manager.Manifest(ctx, "k")
	require.NoError(t, err)
	assert.False(t, manifest.File.StagingMtime.IsZero())
	assert.WithinDuration(t, time.Now(), manifest.File.StagingMtime, time.Minute)
}
