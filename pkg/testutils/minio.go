package testutils

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"
)

// MinIO is a running S3-compatible object store container.
type MinIO struct {
	// Endpoint is the S3 API address (http://host:port).
	Endpoint string

	// AccessKey and Secret are the root credentials.
	AccessKey string
	Secret    string
}

// minioImage is the S3-compatible object store used by integration tests.
// pgsty/silo is a drop-in MinIO replacement.
const minioImage = "docker.io/pgsty/silo:latest"

var (
	minioOnce   sync.Once
	sharedMinio *MinIO
	minioErr    error
)

// StartMinIO returns the shared object store container for the test binary.
func StartMinIO(ctx context.Context, t testing.TB) *MinIO {
	t.Helper()

	SkipWithoutDocker(t)

	minioOnce.Do(func() {
		container, startErr := tcminio.Run(ctx, minioImage,
			tcminio.WithUsername("minioadmin"),
			tcminio.WithPassword("minioadmin"),
		)
		if startErr != nil {
			minioErr = startErr
			return
		}
		endpoint, endpointErr := container.ConnectionString(ctx)
		if endpointErr != nil {
			minioErr = endpointErr
			return
		}
		// ConnectionString is host:port; consumers expect a full HTTP URL.
		sharedMinio = &MinIO{Endpoint: "http://" + endpoint, AccessKey: "minioadmin", Secret: "minioadmin"}
	})

	require.NoError(t, minioErr, "start minio container (docker daemon required)")
	return sharedMinio
}
