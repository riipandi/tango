package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	awshttp "github.com/aws/smithy-go/transport/http"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/testutils"
)

// s3FixtureSalt makes every fixture chunk unique: two runs, or two tests,
// share one bucket, and a hash a previous test left behind must not answer
// this test's probe.
var s3FixtureSalt atomic.Int64

// newS3Store builds the backend over the shared MinIO container, under a
// prefix this test binary owns.
func newS3Store(t *testing.T) (*S3, string, []byte) {
	t.Helper()

	backend := testutils.StartMinIO(t.Context(), t)
	store, err := NewS3(config.S3{
		AccessKeyID:     backend.AccessKey,
		AccessKeySecret: backend.Secret,
		BucketName:      "tango-test",
		EndpointURL:     backend.Endpoint,
		ForcePathStyle:  true,
		Region:          "us-east-1",
		PathPrefix:      "tango-test/",
	})
	require.NoError(t, err)

	// The shared container starts empty: the bucket is this test's to make,
	// and an existing one (a parallel test binary on the same store) is fine.
	if _, err := store.client.CreateBucket(t.Context(), &s3.CreateBucketInput{Bucket: aws.String(store.bucket)}); err != nil {
		var apiErr *awshttp.ResponseError
		if !errors.As(err, &apiErr) || apiErr.HTTPStatusCode() != 409 {
			require.NoError(t, err)
		}
	}

	data := []byte("a chunk served by an S3-compatible store, salt " +
		strconv.FormatInt(s3FixtureSalt.Add(1), 10))
	hash := sha256.Sum256(data)
	return store, hex.EncodeToString(hash[:]), data
}

func TestS3StoreRoundTripsAChunk(t *testing.T) {
	store, hash, data := newS3Store(t)
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

func TestS3StoreGetMissingChunkIsNotFound(t *testing.T) {
	store, hash, _ := newS3Store(t)

	_, err := store.GetChunk(t.Context(), nil, hash)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestS3StoreHasChunksBatchesByPrefix(t *testing.T) {
	store, _, data := newS3Store(t)
	ctx := t.Context()

	// Two chunks whose hashes start with different hex pairs: the batch
	// probe must cross the prefixes in one answer.
	left := []byte("first chunk bytes")
	right := []byte("second chunk bytes, different content")
	leftHash := sha256.Sum256(left)
	rightHash := sha256.Sum256(right)
	leftHex, rightHex := hex.EncodeToString(leftHash[:]), hex.EncodeToString(rightHash[:])
	require.NoError(t, store.PutChunk(ctx, leftHex, left))

	found, err := store.HasChunks(ctx, []string{leftHex, rightHex})
	require.NoError(t, err)
	assert.True(t, found[leftHex])
	assert.False(t, found[rightHex])
	_ = data
}

func TestS3StoreListChunksYieldsTheHashes(t *testing.T) {
	store, hash, data := newS3Store(t)
	ctx := t.Context()

	require.NoError(t, store.PutChunk(ctx, hash, data))

	var listed []string
	require.NoError(t, store.ListChunks(ctx, func(h string) error {
		listed = append(listed, h)
		return nil
	}))
	assert.Contains(t, listed, hash)
}
