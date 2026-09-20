package health_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPool is a Pinger whose answer the test controls.
type stubPool struct {
	err    error
	called bool
}

func (s *stubPool) Ping(context.Context) error {
	s.called = true
	return s.err
}

func TestPostgresCheckPassesWhenPoolPings(t *testing.T) {
	pool := &stubPool{}

	result := health.NewChecker(health.WithCheck(health.PostgresCheck(pool, "localhost:5432/test"))).Check(t.Context())

	assert.True(t, pool.called)
	assert.Equal(t, health.GlobalHealthy, result.Status)
	assert.Equal(t, health.CheckNamePostgres, result.Details["postgres"].Name)
}

// The target must be reported, so the text output says which database was
// reached, and must never carry the password from the DSN.
func TestPostgresCheckReportsTarget(t *testing.T) {
	check := health.PostgresCheck(&stubPool{}, "localhost:5432/test")

	assert.Equal(t, "localhost:5432/test", check.Target)
	assert.NotContains(t, check.Target, "@")
}

// The error must name Postgres, so a failing probe says which dependency is
// down without the caller inspecting the check set.
func TestPostgresCheckWrapsPingError(t *testing.T) {
	pool := &stubPool{err: errors.New("connection refused")}

	result := health.NewChecker(health.WithCheck(health.PostgresCheck(pool, "localhost:5432/test"))).Check(t.Context())

	assert.Equal(t, health.GlobalUnhealthy, result.Status)
	assert.Contains(t, result.Details["postgres"].Error, "postgres")
	assert.Contains(t, result.Details["postgres"].Error, "connection refused")
}

// The check must be required, so a dead database fails the probe rather than
// being reported as an optional extra.
func TestPostgresCheckIsRequired(t *testing.T) {
	check := health.PostgresCheck(&stubPool{}, "localhost:5432/test")
	assert.False(t, check.Optional)

	// It carries its own timeout, shorter than the checker default, so a
	// hanging database still leaves time to report the failure.
	assert.Positive(t, check.Timeout)
	assert.Less(t, check.Timeout, health.DefaultTimeout)
}

// The check must work against the real adapter, not only a stub: this is the
// interface the CLI and the server wire up.
func TestPostgresCheckAgainstRealPool(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	dsn := container.NewDatabase(t)

	pool, err := datastore.NewPostgres(t.Context(), datastore.PostgresOptions{DSN: dsn})
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	result := health.NewChecker(health.WithCheck(health.PostgresCheck(pool, "localhost:5432/test"))).Check(t.Context())
	assert.Equal(t, health.GlobalHealthy, result.Status)
	assert.Empty(t, result.Failed())

	// Once the pool is closed the probe must fail, which is what a lost
	// database looks like from the process.
	pool.Close()

	result = health.NewChecker(
		health.WithCheck(health.PostgresCheck(pool, "localhost:5432/test")),
		health.WithTimeout(5*time.Second),
	).Check(t.Context())
	assert.Equal(t, health.GlobalUnhealthy, result.Status)
	assert.Contains(t, result.Details["postgres"].Error, "postgres")
}
