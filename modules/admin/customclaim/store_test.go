package customclaim

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the claim stack over the shared test container
// with real stores.
func newTestStack(t *testing.T) (*PostgresStore, *user.PostgresStore, *usergroup.PostgresStore) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	return NewPostgresStore(ds), user.NewPostgresStore(ds), usergroup.NewPostgresStore(ds)
}

func TestClaimsForUserAndGroup(t *testing.T) {
	store, users, groups := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	u, err := users.Create(ctx, user.CreateParams{
		Username: "claim_u_" + stamp, Email: "claim-u-" + stamp + "@example.com"})
	require.NoError(t, err)
	g, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "claim_g_" + stamp, DisplayName: "Claim Group"})
	require.NoError(t, err)

	// User scope.
	claim, err := store.Create(ctx, UpsertParams{Key: "role", Value: "vip", UserID: strPtr(u.ID.UUID())})
	require.NoError(t, err)
	assert.False(t, claim.ID.IsZero())

	dup, err := store.ExistsForOwner(ctx, UpsertParams{Key: "role", UserID: strPtr(u.ID.UUID())})
	require.NoError(t, err)
	assert.True(t, dup, "existing pair must be detected")

	list, err := store.ListByUser(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "vip", list[0].Value)

	// Update + delete.
	updated, err := store.Update(ctx, claim.ID, "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", updated.Value)
	require.NoError(t, store.Delete(ctx, claim.ID))

	// Group scope, same key: independent of the user scope.
	gClaim, err := store.Create(ctx, UpsertParams{Key: "role", Value: "member", UserGroupID: strPtr(g.ID.UUID())})
	require.NoError(t, err)
	gList, err := store.ListByGroup(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, gList, 1)
	assert.Equal(t, gClaim.ID, gList[0].ID)

	keys, err := store.SuggestedKeys(ctx)
	require.NoError(t, err)
	assert.Contains(t, keys, "role")
}

func strPtr(s string) *string { return &s }
