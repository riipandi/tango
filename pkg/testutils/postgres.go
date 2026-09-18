package testutils

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	tcpg "github.com/testcontainers/testcontainers-go/modules/postgres"
)

// Postgres is a running Postgres container for integration tests.
type Postgres struct {
	// DSN is the connection string of the shared instance.
	DSN string
}

// postgresImage is the Postgres image used by integration tests.
const postgresImage = "docker.io/postgres:18-alpine"

var (
	postgresOnce   sync.Once
	sharedPostgres *Postgres
	sharedPGErr    error
)

// StartPostgres returns the shared Postgres container for the test binary.
func StartPostgres(ctx context.Context, t testing.TB) *Postgres {
	t.Helper()

	postgresOnce.Do(func() {
		sharedPostgres, sharedPGErr = startPostgres(ctx)
	})

	require.NoError(t, sharedPGErr, "start postgres container (docker daemon required)")
	return sharedPostgres
}

// startPostgres starts the container. BasicWaitStrategies waits for the
// "ready to accept connections" log twice (the image restarts once during
// init) and then for the port to be served — required for reliable startup
// on Docker Desktop proxies (macOS/Windows).
func startPostgres(ctx context.Context) (*Postgres, error) {
	container, err := tcpg.Run(ctx, postgresImage,
		tcpg.WithDatabase("tango_test"),
		tcpg.WithUsername("postgres"),
		tcpg.WithPassword("postgres"),
		tcpg.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, fmt.Errorf("run postgres container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, fmt.Errorf("resolve postgres endpoint: %w", err)
	}
	return &Postgres{DSN: dsn}, nil
}
