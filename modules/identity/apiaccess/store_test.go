package apiaccess

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the API store over the shared test container
// plus a fresh OIDC client for grant tests.
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

	client := oidc.NewID().String()
	_, err = ds.Exec(ctx,
		"INSERT INTO public.oidc_clients (id, name) VALUES ($1, $2)", client, "Test Client")
	require.NoError(t, err)

	return NewPostgresStore(ds), client
}

func TestAPICRUD(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	created, err := store.Create(ctx, CreateParams{Name: "Billing " + stamp, Resource: "billing_" + stamp})
	require.NoError(t, err)
	assert.Equal(t, "api", created.ID.Prefix())
	assert.False(t, created.AllowCIMDClients)

	got, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.Resource, got.Resource)
	assert.Empty(t, got.Permissions)

	updated, err := store.Update(ctx, created.ID, UpdateParams{Name: "Billing Renamed"})
	require.NoError(t, err)
	assert.Equal(t, "Billing Renamed", updated.Name)

	require.NoError(t, store.Delete(ctx, created.ID))
	_, err = store.GetByID(ctx, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestAPIDuplicateResource(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := store.Create(ctx, CreateParams{Name: "A", Resource: "dup_" + stamp})
	require.NoError(t, err)
	_, err = store.Create(ctx, CreateParams{Name: "B", Resource: "dup_" + stamp})
	assert.ErrorIs(t, err, ErrDuplicate)
}

func TestSetPermissionsListReplace(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	a, err := store.Create(ctx, CreateParams{Name: "Perms " + stamp, Resource: "perms_" + stamp})
	require.NoError(t, err)

	first, err := store.SetPermissions(ctx, a.ID, []PermissionInput{
		{Key: "read", Name: "Read"},
		{Key: "write", Name: "Write", Description: ptr("Write access")},
	})
	require.NoError(t, err)
	require.Len(t, first, 2)

	got, err := store.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, got.Permissions, 2)
	assert.Equal(t, "read", got.Permissions[0].Key) // ordered by key

	second, err := store.SetPermissions(ctx, a.ID, []PermissionInput{
		{Key: "admin", Name: "Admin"},
	})
	require.NoError(t, err)
	require.Len(t, second, 1)

	got, err = store.GetByID(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, got.Permissions, 1)
	assert.Equal(t, "admin", got.Permissions[0].Key)
}

func TestGrantLifecycle(t *testing.T) {
	store, clientID := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	a, err := store.Create(ctx, CreateParams{Name: "Grants " + stamp, Resource: "grants_" + stamp})
	require.NoError(t, err)
	perms, err := store.SetPermissions(ctx, a.ID, []PermissionInput{
		{Key: "read", Name: "Read"},
		{Key: "write", Name: "Write"},
	})
	require.NoError(t, err)

	// No grant yet → assignable.
	_, assignableBefore, err := store.ListAssignableAPIs(ctx, clientID, ListParams{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, assignableBefore, 1)

	_, err = store.GrantFor(ctx, a.ID, clientID)
	assert.ErrorIs(t, err, ErrNotFound)

	grant, err := store.UpsertGrant(ctx, a.ID, clientID, GrantParams{
		UserDelegatedAccess:        true,
		UserDelegatedPermissionIDs: []string{perms[0].ID},
		ClientAccess:               true,
		ClientPermissionIDs:        []string{perms[0].ID, perms[1].ID},
	})
	require.NoError(t, err)
	assert.True(t, grant.UserDelegatedAccess)
	assert.Len(t, grant.UserDelegatedPermissionIDs, 1)
	assert.Len(t, grant.ClientPermissionIDs, 2)

	// Re-upsert replaces the lists.
	grant, err = store.UpsertGrant(ctx, a.ID, clientID, GrantParams{
		ClientAccess:        true,
		ClientPermissionIDs: []string{perms[1].ID},
	})
	require.NoError(t, err)
	assert.False(t, grant.UserDelegatedAccess)
	assert.Empty(t, grant.UserDelegatedPermissionIDs)
	assert.Equal(t, []string{perms[1].ID}, grant.ClientPermissionIDs)

	// Unknown permission ID is rejected.
	_, err = store.UpsertGrant(ctx, a.ID, clientID, GrantParams{
		ClientAccess:        true,
		ClientPermissionIDs: []string{"00000000-0000-0000-0000-000000000000"},
	})
	assert.ErrorIs(t, err, ErrUnknownPerms)

	// Unknown client is rejected.
	_, err = store.UpsertGrant(ctx, a.ID, "nope", GrantParams{})
	assert.ErrorIs(t, err, ErrUnknownClient)

	// Client listing sees the grant; assignable flips empty for
	// this API (other tests share the container).
	clients, total, err := store.ListClientsWithAccess(ctx, a.ID, ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Equal(t, clientID, clients[0].ID)

	_, total, err = store.ListAssignableClients(ctx, a.ID, ListParams{})
	require.NoError(t, err)
	assert.Equal(t, 0, total)

	_, assignableAfter, err := store.ListAssignableAPIs(ctx, clientID, ListParams{})
	require.NoError(t, err)
	assert.Equal(t, assignableBefore-1, assignableAfter)

	grants, err := store.ListGrantsForClient(ctx, clientID)
	require.NoError(t, err)
	require.Len(t, grants, 1)
	assert.Equal(t, a.ID.String(), grants[0].APIID)

	require.NoError(t, store.DeleteGrant(ctx, a.ID, clientID))
	_, err = store.GrantFor(ctx, a.ID, clientID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetCIMDAccess(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	a, err := store.Create(ctx, CreateParams{Name: "CIMD " + stamp, Resource: "cimd_" + stamp})
	require.NoError(t, err)
	perms, err := store.SetPermissions(ctx, a.ID, []PermissionInput{
		{Key: "read", Name: "Read"},
		{Key: "write", Name: "Write"},
	})
	require.NoError(t, err)

	updated, err := store.SetCIMDAccess(ctx, a.ID, true, []string{perms[0].ID})
	require.NoError(t, err)
	assert.True(t, updated.AllowCIMDClients)
	assert.True(t, updated.Permissions[0].AllowedForCIMDClients)
	assert.False(t, updated.Permissions[1].AllowedForCIMDClients)

	// Toggling off clears every permission flag.
	updated, err = store.SetCIMDAccess(ctx, a.ID, false, nil)
	require.NoError(t, err)
	assert.False(t, updated.AllowCIMDClients)
	for _, p := range updated.Permissions {
		assert.False(t, p.AllowedForCIMDClients)
	}
}

func TestListPagination(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	for range 3 {
		_, err := store.Create(ctx, CreateParams{Name: "Page " + stamp, Resource: "page_" + stamp + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)})
		require.NoError(t, err)
	}

	apis, total, err := store.List(ctx, ListParams{Query: stamp, PaginationParams: responder.PaginationParams{Page: 1, Limit: 2}})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, apis, 2)
}

func ptr(s string) *string { return &s }
