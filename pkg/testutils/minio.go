package testutils

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tcre "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// MinIO is a running MinIO container: an S3-compatible server used to
// exercise the storage package's S3 backend.
type MinIO struct {
	// Endpoint is the S3 API address (http://host:port).
	Endpoint string

	// AccessKey and Secret are the root credentials.
	AccessKey string
	Secret    string
}

// minioImage is intentionally unpinned to match the compose stack.
const minioImage = "minio/minio:latest"

var (
	minioOnce   sync.Once
	sharedMinio *MinIO
	minioErr    error
)

// StartMinIO returns the process-wide shared MinIO container: the
// first call starts it, later calls in the same test binary reuse it,
// and the testcontainers reaper terminates it when the binary exits.
// Tests isolate their data with unique buckets.
func StartMinIO(ctx context.Context, t testing.TB) *MinIO {
	t.Helper()

	minioOnce.Do(func() {
		container, startErr := tcre.Run(ctx, minioImage,
			tcre.WithEnv(map[string]string{
				"MINIO_ROOT_USER":     "minioadmin",
				"MINIO_ROOT_PASSWORD": "minioadmin",
			}),
			tcre.WithCmd("server", "/data"),
			tcre.WithAdditionalWaitStrategy(
				wait.ForHTTP("/minio/health/ready").
					WithPort("9000/tcp").
					WithStartupTimeout(60*time.Second),
			),
		)
		if startErr != nil {
			minioErr = startErr
			return
		}
		endpoint, endpointErr := container.PortEndpoint(ctx, "9000/tcp", "http")
		if endpointErr != nil {
			minioErr = endpointErr
			return
		}
		sharedMinio = &MinIO{Endpoint: endpoint, AccessKey: "minioadmin", Secret: "minioadmin"}
	})

	require.NoError(t, minioErr, "start minio container (docker daemon required)")
	return sharedMinio
}
