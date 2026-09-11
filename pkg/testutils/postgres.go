package testutils

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/moby/moby/api/types/network"
	"github.com/stretchr/testify/require"
	tcre "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	// Registers the "pgx" database/sql driver used by wait.ForSQL.
	_ "github.com/jackc/pgx/v5/stdlib"
)

// Postgres is a running Postgres container for integration tests.
type Postgres struct {
	// DSN is the connection string of the shared instance
	// (sslmode=disable, password auth only on loopback).
	DSN string
}

// postgresImage intentionally matches compose.yaml's pgsql service;
// pin both when pinning one.
const postgresImage = "postgres:18-alpine"

var (
	postgresOnce   sync.Once
	sharedPostgres *Postgres
	sharedPGErr    error
)

// StartPostgres returns the process-wide shared Postgres container:
// the first call starts it, later calls in the same test binary
// reuse it, and the testcontainers reaper terminates it when the
// binary exits. Tests isolate their data with unique table names.
func StartPostgres(ctx context.Context, t testing.TB) *Postgres {
	t.Helper()

	postgresOnce.Do(func() {
		sharedPostgres, sharedPGErr = startPostgres(ctx)
	})

	require.NoError(t, sharedPGErr, "start postgres container (docker daemon required)")
	return sharedPostgres
}

// startPostgres boots the container and blocks until the server
// answers a real SQL connection, not just a listening port.
func startPostgres(ctx context.Context) (*Postgres, error) {
	container, err := tcre.Run(ctx, postgresImage,
		tcre.WithEnv(map[string]string{
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_DB":       "tango_test",
		}),
		tcre.WithAdditionalWaitStrategy(
			wait.ForSQL("5432/tcp", "pgx", func(host string, port network.Port) string {
				return fmt.Sprintf("postgresql://postgres:postgres@%s:%s/tango_test?sslmode=disable", host, port.Port())
			}).WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("run postgres container: %w", err)
	}

	addr, err := container.PortEndpoint(ctx, "5432/tcp", "")
	if err != nil {
		return nil, fmt.Errorf("resolve postgres endpoint: %w", err)
	}

	host, dbPort, splitErr := net.SplitHostPort(addr)
	if splitErr != nil {
		return nil, fmt.Errorf("resolve postgres endpoint: %w", splitErr)
	}
	return &Postgres{
		DSN: fmt.Sprintf("postgresql://postgres:postgres@%s:%s/tango_test?sslmode=disable", host, dbPort),
	}, nil
}
