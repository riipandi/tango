package apikey

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the key store over the shared test container
// plus a real user row to own the keys.
func newTestStack(t *testing.T) (*PostgresStore, string) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	users := user.NewPostgresStore(ds)
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(ctx, user.CreateParams{
		Username:    "keyuser" + stamp[len(stamp)-8:],
		Email:       "keyuser" + stamp + "@test.local",
		DisplayName: "Key User",
	})
	require.NoError(t, err)

	return NewPostgresStore(ds), u.ID.String()
}

func TestKeyLifecycle(t *testing.T) {
	store, userID := newTestStack(t)
	ctx := t.Context()

	expires := time.Now().Add(24 * time.Hour).UTC().Truncate(time.Second)
	created, err := store.Create(ctx, userID, "hash-one", CreateParams{
		Name:        "CI Key",
		Description: ptr("for tests"),
		ExpiresAt:   expires,
	})
	require.NoError(t, err)
	assert.Equal(t, "api_key", created.ID.Prefix())

	// Resolve the token hash to its owner; last_used_at set.
	k, u, err := store.ValidByHash(ctx, "hash-one")
	require.NoError(t, err)
	assert.Equal(t, created.ID.String(), k.ID.String())
	assert.Equal(t, userID, u.ID.String())
	assert.NotNil(t, k.LastUsedAt)

	// Expired hash resolves nothing.
	_, _, err = store.ValidByHash(ctx, "hash-missing")
	assert.ErrorIs(t, err, ErrInvalidCreds)

	// Renewal of an unexpired key is rejected.
	_, err = store.Renew(ctx, userID, created.ID, "hash-two", expires.Add(48*time.Hour))
	assert.ErrorIs(t, err, ErrNotExpired)

	keys, total, err := store.ListForUser(ctx, userID, ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	require.Len(t, keys, 1)
	assert.Equal(t, "CI Key", keys[0].Name)

	require.NoError(t, store.Revoke(ctx, userID, created.ID))
	_, _, err = store.ValidByHash(ctx, "hash-one")
	assert.ErrorIs(t, err, ErrInvalidCreds)
}

func TestKeyDuplicateName(t *testing.T) {
	store, userID := newTestStack(t)
	ctx := t.Context()
	expires := time.Now().Add(24 * time.Hour).UTC()

	_, err := store.Create(ctx, userID, "hash-a", CreateParams{Name: "Same Name", ExpiresAt: expires})
	require.NoError(t, err)
	_, err = store.Create(ctx, userID, "hash-b", CreateParams{Name: "Same Name", ExpiresAt: expires})
	assert.ErrorIs(t, err, ErrDuplicate)
}

func TestKeyPagination(t *testing.T) {
	store, userID := newTestStack(t)
	ctx := t.Context()
	expires := time.Now().Add(24 * time.Hour).UTC()

	for i := range 3 {
		_, err := store.Create(ctx, userID, "hash-p"+strconv.Itoa(i), CreateParams{
			Name:      "Key " + strconv.Itoa(i),
			ExpiresAt: expires,
		})
		require.NoError(t, err)
	}

	keys, total, err := store.ListForUser(ctx, userID, ListParams{
		PaginationParams: responder.PaginationParams{Page: 1, Limit: 2},
	})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, keys, 2)
}

func ptr(s string) *string { return &s }
