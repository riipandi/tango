package queue

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riipandi/tango/pkg/antree"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLifecycle runs the dispatcher briefly against the shared test
// Postgres: Start is idempotent-safe to call once, Stop drains idle
// workers cleanly.
func TestLifecycle(t *testing.T) {
	pg := testutils.StartPostgres(t.Context(), t)

	pool, err := pgxpool.New(t.Context(), pg.DSN)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	client, err := antree.NewClient(antree.ClientConfig{
		DB:           pool,
		NumWorkers:   1,
		ReleaseAfter: 30 * time.Second,
	})
	require.NoError(t, err)

	m := New(client)
	assert.Equal(t, "queue", m.Name())

	require.NoError(t, m.Start(t.Context()))
	require.NoError(t, m.Stop(t.Context()))
}
