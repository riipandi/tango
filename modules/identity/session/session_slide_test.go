package session

import (
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestResolveTouchesSlidingExpiry pins the sliding-refresh contract on
// a session past half-life: resolution must succeed, refresh the
// expiry, and survive the raw-UUID id form the database default
// writes. A TypeID string leaking into the UUID column broke every
// long-lived session here.
func TestResolveTouchesSlidingExpiry(t *testing.T) {
	ctx := t.Context()
	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	users := user.NewPostgresStore(ds)
	u, err := users.Create(ctx, user.CreateParams{
		Username:    "slider",
		Email:       "slider@example.com",
		DisplayName: "Slider",
	})
	require.NoError(t, err)

	token := "sliding-expiry-token-0123456789"
	_, err = ds.Pool().Exec(ctx,
		`INSERT INTO public.sessions (user_id, provider, token_hash, expires_at)
		 VALUES ($1, 'password', $2, NOW() + INTERVAL '1 hour')`, u.ID.UUIDBytes(), hashToken(token))
	require.NoError(t, err)

	svc := NewService(NewPostgresStore(ds), nil, nil, nil)
	pu, _, rerr := svc.Resolve(ctx, token)
	require.NoError(t, rerr)
	assert.Equal(t, u.ID.String(), pu.ID.String())

	// The touch refreshed the expiry beyond the seeded window.
	row := ds.Pool().QueryRow(ctx, `SELECT expires_at FROM public.sessions WHERE token_hash = $1`, hashToken(token))
	var expiresAt time.Time
	require.NoError(t, row.Scan(&expiresAt))
	assert.Greater(t, expiresAt, time.Now().Add(25*24*time.Hour),
		"touch must extend a half-life session by the full lifetime")
}
