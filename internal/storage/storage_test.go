package storage

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// roundTrip exercises the Store contract against any backend.
func roundTrip(t *testing.T, store Store) {
	t.Helper()
	ctx := t.Context()

	require.NoError(t, store.Save(ctx, "images/logo.png", bytes.NewBufferString("logo-data")))

	require.NoError(t, store.Save(ctx, "images/logo.png", bytes.NewBufferString("logo-data")))

	reader, size, err := store.Open(ctx, "images/logo.png")
	require.NoError(t, err)
	contents, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	assert.Equal(t, "logo-data", string(contents))
	assert.Equal(t, int64(len(contents)), size)

	// Overwrite is allowed and atomic.
	require.NoError(t, store.Save(ctx, "images/logo.png", bytes.NewBufferString("v2")))
	reader, _, _ = store.Open(ctx, "images/logo.png")
	contents, _ = io.ReadAll(reader)
	reader.Close()
	assert.Equal(t, "v2", string(contents))

	// Nested save and flat listing.
	require.NoError(t, store.Save(ctx, "images/nested/child.txt", bytes.NewBufferString("child")))
	files, err := store.List(ctx, "images")
	require.NoError(t, err)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	assert.ElementsMatch(t, []string{"images/logo.png", "images/nested/child.txt"}, paths)

	_, _, err = store.Open(ctx, "missing/object.bin")
	require.Error(t, err)
	assert.True(t, IsNotExist(err), "expected not-exist, got %v", err)

	require.NoError(t, store.DeleteAll(ctx, "images"))
	_, err = store.List(ctx, "images")
	assert.True(t, IsNotExist(err), "prefix should be gone, got %v", err)
}

func TestFilesystemStorage(t *testing.T) {
	store, err := NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)
	assert.Equal(t, TypeFilesystem, store.Type())
	roundTrip(t, store)
}

func TestFilesystemRejectsEscape(t *testing.T) {
	store, err := NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)

	err = store.Save(t.Context(), "../escape.txt", strings.NewReader("nope"))
	require.Error(t, err, "path escape must fail")
}

func TestS3ObjectKey(t *testing.T) {
	s := &s3Storage{bucket: "bucket", prefix: "root"}
	assert.Equal(t, "root", s.objectKey(""))
	assert.Equal(t, "root/foo/bar", s.objectKey("/foo//bar/"))
	assert.Equal(t, "root/images/logo.png", s.objectKey("./images/logo.png"))

	noPrefix := &s3Storage{bucket: "bucket"}
	assert.Equal(t, "foo/bar", noPrefix.objectKey("/foo/bar/"))
}

func TestPicker(t *testing.T) {
	// No endpoint → filesystem under a temp dir.
	fsCfg := newStorageConfig(false)
	fsCfg.DataDir = t.TempDir()
	store, err := New(fsCfg)
	require.NoError(t, err)
	assert.Equal(t, TypeFilesystem, store.Type())

	// Endpoint set → S3 backend (no network at construction).
	store, err = New(newStorageConfig(true))
	require.NoError(t, err)
	assert.Equal(t, TypeS3, store.Type())
}

func newStorageConfig(withS3 bool) config.StorageConfig {
	c := config.StorageConfig{}
	if withS3 {
		c.S3EndpointURL = "http://localhost:9100"
		c.S3BucketDefault = "devbucket"
		c.S3Region = "auto"
		c.S3ForcePathStyle = true
	}
	return c
}
