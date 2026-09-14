package storage

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/testutils"
)

// roundTrip exercises the Store contract against any backend.
func roundTrip(t *testing.T, store Store) {
	t.Helper()
	ctx := t.Context()

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

	// Single-object delete.
	require.NoError(t, store.Delete(ctx, "images/nested/child.txt"))
	_, _, err = store.Open(ctx, "images/nested/child.txt")
	assert.True(t, IsNotExist(err), "deleted object should be gone, got %v", err)

	_, _, err = store.Open(ctx, "missing/object.bin")
	require.Error(t, err)
	assert.True(t, IsNotExist(err), "expected not-exist, got %v", err)

	// Deleting a missing object is not an error (idempotent).
	assert.NoError(t, store.Delete(ctx, "images/nested/child.txt"))

	require.NoError(t, store.DeleteAll(ctx, "images"))
	// FS reports the missing prefix as not-exist; S3 as an empty page.
	files, err = store.List(ctx, "images")
	if err != nil {
		assert.True(t, IsNotExist(err), "prefix should be gone, got %v", err)
	} else {
		assert.Empty(t, files, "prefix should be gone")
	}
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

func TestFilesystemErrorBranches(t *testing.T) {
	// Root creation fails when the parent is a file.
	file := t.TempDir() + "/file"
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	_, err := NewFilesystemStorage(file + "/root")
	assert.Error(t, err, "a file cannot host the storage root")

	// Saving under a path that already exists as a file errors out.
	store, err := NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)
	ctx := t.Context()
	require.NoError(t, store.Save(ctx, "conflict", strings.NewReader("file")))
	err = store.Save(ctx, "conflict/child", strings.NewReader("under a file"))
	assert.Error(t, err, "a file cannot become a directory")

	// The original file survived the failed save.
	reader, size, err := store.Open(ctx, "conflict")
	require.NoError(t, err)
	contents, err := io.ReadAll(reader)
	reader.Close()
	require.NoError(t, err)
	assert.Equal(t, "file", string(contents))
	assert.Equal(t, int64(4), size)

	// A reader that fails mid-stream aborts the save and leaves no
	// temp file or object behind.
	assert.Error(t, store.Save(ctx, "doomed", failingReader{}))
	_, _, err = store.Open(ctx, "doomed")
	assert.True(t, IsNotExist(err), "a failed save must not leave an object")
	assert.Equal(t, []string{"conflict"}, listPaths(t, store, ""))

	// Deleting the whole root is refused.
	assert.Error(t, store.DeleteAll(ctx, ""))
}

// failingReader errors after the first byte.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, assert.AnError }

func listPaths(t *testing.T, store Store, prefix string) []string {
	t.Helper()
	files, err := store.List(t.Context(), prefix)
	require.NoError(t, err)
	paths := make([]string, 0, len(files))
	for _, f := range files {
		paths = append(paths, f.Path)
	}
	return paths
}

// TestS3StorageContract runs the full Store contract against MinIO
// (docker daemon required).
func TestS3StorageContract(t *testing.T) {
	ctx := t.Context()
	minio := testutils.StartMinIO(ctx, t)

	bucket := fmt.Sprintf("contract-%d", time.Now().UnixNano())
	client := s3.New(s3.Options{
		Region:       "auto",
		BaseEndpoint: aws.String(minio.Endpoint),
		Credentials: credentials.NewStaticCredentialsProvider(
			minio.AccessKey, minio.Secret, ""),
		UsePathStyle:               true,
		RetryMaxAttempts:           3,
		RetryMode:                  aws.RetryModeStandard,
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	})
	_, err := client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(bucket)})
	require.NoError(t, err)

	store, err := NewS3Storage(S3Config{
		Bucket:          bucket,
		Region:          "auto",
		Endpoint:        minio.Endpoint,
		AccessKeyID:     minio.AccessKey,
		SecretAccessKey: minio.Secret,
		ForcePathStyle:  true,
		Root:            "root",
	})
	require.NoError(t, err)
	assert.Equal(t, TypeS3, store.Type())

	// The root prefix must not leak into the contract paths.
	roundTrip(t, store)
}

func TestS3ObjectKey(t *testing.T) {
	s := &s3Storage{bucket: "bucket", prefix: "root"}
	assert.Equal(t, "root", s.objectKey(""))
	assert.Equal(t, "root/foo/bar", s.objectKey("/foo//bar/"))
	assert.Equal(t, "root/images/logo.png", s.objectKey("./images/logo.png"))

	noPrefix := &s3Storage{bucket: "bucket"}
	assert.Equal(t, "foo/bar", noPrefix.objectKey("/foo/bar/"))
	assert.Equal(t, "foo/bar", noPrefix.pathFromKey("foo/bar"), "no prefix: keys pass through")
	assert.Equal(t, "b", (&s3Storage{bucket: "bucket", prefix: "root"}).pathFromKey("root/b"))
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
		prefix := "tenant"
		c.S3EndpointURL = "http://localhost:9100"
		c.S3BucketDefault = "devbucket"
		c.S3Region = "auto"
		c.S3ForcePathStyle = true
		c.S3PathPrefix = &prefix
	}
	return c
}
