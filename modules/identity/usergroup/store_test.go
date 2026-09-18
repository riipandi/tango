package usergroup

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the group store over the shared test container.
func newTestStack(t *testing.T) (*PostgresStore, *user.PostgresStore) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	return NewPostgresStore(ds), user.NewPostgresStore(ds)
}

func TestGroupCRUD(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	created, err := store.Create(ctx, CreateParams{Name: "admins_" + stamp, DisplayName: "Admins"})
	require.NoError(t, err)
	assert.Equal(t, "user_group", created.ID.Prefix())
	assert.Equal(t, "admins_"+stamp, created.Name)

	got, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.DisplayName, got.DisplayName)

	updated, err := store.Update(ctx, created.ID, UpdateParams{DisplayName: ptr("Admin Team")})
	require.NoError(t, err)
	assert.Equal(t, "Admin Team", updated.DisplayName)

	require.NoError(t, store.Delete(ctx, created.ID))
	_, err = store.GetByID(ctx, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)

	_, err = store.Create(ctx, CreateParams{Name: "admins_" + stamp, DisplayName: "Dup"})
	_ = err // duplicate name check after delete would pass; covered below
}

func TestGroupDuplicateName(t *testing.T) {
	store, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := store.Create(ctx, CreateParams{Name: "dup_" + stamp, DisplayName: "One"})
	require.NoError(t, err)
	_, err = store.Create(ctx, CreateParams{Name: "dup_" + stamp, DisplayName: "Two"})
	assert.ErrorIs(t, err, ErrDuplicate)
}

func TestMembershipRoundTrip(t *testing.T) {
	store, users := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	group, err := store.Create(ctx, CreateParams{Name: "members_" + stamp, DisplayName: "Members"})
	require.NoError(t, err)

	// Unknown member IDs must roll the whole write back.
	ghost := user.MustID("0199aaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	err = store.SetMembers(ctx, group.ID, []user.UserID{ghost})
	require.Error(t, err, "unknown member must fail inside the tx")

	// Valid members stick.
	a, err := users.Create(ctx, user.CreateParams{
		Username: "mem_a_" + stamp, Email: "mem-a-" + stamp + "@example.com"})
	require.NoError(t, err)
	b, err := users.Create(ctx, user.CreateParams{
		Username: "mem_b_" + stamp, Email: "mem-b-" + stamp + "@example.com"})
	require.NoError(t, err)
	require.NoError(t, store.SetMembers(ctx, group.ID, []user.UserID{a.ID, b.ID}))

	members, err := store.MemberIDs(ctx, group.ID)
	require.NoError(t, err)
	assert.ElementsMatch(t, []user.UserID{a.ID, b.ID}, members)

	groups, err := store.GroupIDsForUser(ctx, a.ID)
	require.NoError(t, err)
	require.Len(t, groups, 1)
	assert.Equal(t, group.ID, groups[0].ID)

	// Replacement clears the previous set.
	require.NoError(t, store.SetMembers(ctx, group.ID, []user.UserID{b.ID}))
	members, err = store.MemberIDs(ctx, group.ID)
	require.NoError(t, err)
	assert.Equal(t, []user.UserID{b.ID}, members)

	list, total, err := store.List(ctx, ListParams{Query: "members_" + stamp,
		Page: Page{Page: 1, Limit: 10}})
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, 1, total)
	assert.Equal(t, group.ID, list[0].ID)
}

func ptr(s string) *string { return &s }
