package datastore

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testDSN = "postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable"

// TestNewRequiresDSN fails fast on an empty DSN — a silent default
// would point the pool at an unintended local server.
func TestNewRequiresDSN(t *testing.T) {
	store, err := New(t.Context(), Options{DSN: ""})

	require.Error(t, err)
	assert.ErrorContains(t, err, "DSN is required")
	assert.Nil(t, store)
}

// TestPoolConfigRejectsMalformedDSN surfaces DSN syntax errors as
// construction failures, not as query-time surprises.
func TestPoolConfigRejectsMalformedDSN(t *testing.T) {
	_, err := poolConfig(Options{DSN: "://not-a-dsn"})

	require.Error(t, err)
	assert.ErrorContains(t, err, "parse DSN")
}

// TestPoolConfigSessionSettings locks in the session-wide runtime
// parameters and the fixed connect timeout.
func TestPoolConfigSessionSettings(t *testing.T) {
	cfg, err := poolConfig(Options{DSN: testDSN})

	require.NoError(t, err)
	assert.Equal(t, PgSearchPath, cfg.ConnConfig.RuntimeParams["search_path"])
	assert.Equal(t, PgTimezone, cfg.ConnConfig.RuntimeParams["timezone"])
	assert.Equal(t, 10*time.Second, cfg.ConnConfig.ConnectTimeout)
}

// TestPoolConfigDefaultsFromDSN keeps the pgx path intact: with all
// Options knobs unset, pool sizing follows the DSN parameters.
func TestPoolConfigDefaultsFromDSN(t *testing.T) {
	cfg, err := poolConfig(Options{
		DSN: testDSN + "&pool_max_conns=3&pool_min_conns=1",
	})

	require.NoError(t, err)
	assert.Equal(t, int32(3), cfg.MaxConns)
	assert.Equal(t, int32(1), cfg.MinConns)
	assert.Greater(t, cfg.MaxConnLifetime, time.Duration(0), "pgx default lifetime preserved")
	assert.Greater(t, cfg.MaxConnIdleTime, time.Duration(0), "pgx default idle time preserved")
}

// TestPoolConfigOptionsOverrideDSN gives the explicit knobs
// precedence over the DSN parameters.
func TestPoolConfigOptionsOverrideDSN(t *testing.T) {
	cfg, err := poolConfig(Options{
		DSN:             testDSN + "&pool_max_conns=3&pool_min_conns=1",
		MaxConnections:  42,
		MinConnections:  7,
		MaxConnLifetime: 30 * time.Minute,
		MaxConnIdleTime: 5 * time.Minute,
	})

	require.NoError(t, err)
	assert.Equal(t, int32(42), cfg.MaxConns)
	assert.Equal(t, int32(7), cfg.MinConns)
	assert.Equal(t, 30*time.Minute, cfg.MaxConnLifetime)
	assert.Equal(t, 5*time.Minute, cfg.MaxConnIdleTime)
}
