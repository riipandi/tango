package ldapsync

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newReconciler builds the reconciler over the shared test container.
func newReconciler(t *testing.T) *Reconciler {
	t.Helper()
	pg := testutils.StartPostgres(t.Context(), t)
	if _, err := database.MigrateUp(t.Context(), pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(t.Context(), datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })
	return NewReconciler(ds)
}

func stamp() string {
	s := strconv.FormatInt(time.Now().UnixNano(), 10)
	return s[len(s)-10:]
}

func desiredUsers() []desiredUser {
	s := stamp()
	return []desiredUser{
		{LDAPID: "u1-" + s, Username: "ada" + s, Email: "ada." + s + "@tango.local", FirstName: "Ada", LastName: "Wong"},
		{LDAPID: "u2-" + s, Username: "budi" + s, Email: "budi." + s + "@tango.local", FirstName: "Budi"},
	}
}

func TestSyncLifecycle(t *testing.T) {
	r := newReconciler(t)
	ctx := t.Context()

	// Seed: two users, two groups, first group has both members.
	users := desiredUsers()
	groupName := "engineering" + stamp()
	state := []desiredGroup{
		{LDAPID: "g1-" + stamp(), Name: groupName, Members: []string{users[0].Username, users[1].Username}},
	}

	stats, err := r.Run(ctx, users, state, false)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.UsersCreated)
	assert.Equal(t, 1, stats.GroupsCreated)

	// Idempotent re-run: no changes.
	stats, err = r.Run(ctx, users, state, false)
	require.NoError(t, err)
	assert.Zero(t, stats.UsersCreated)
	assert.Zero(t, stats.UsersDeleted)
	assert.Zero(t, stats.GroupsCreated)
	assert.Zero(t, stats.GroupsDeleted)

	// Remove one user from LDAP state; hard delete removes the row.
	remaining := users[:1]
	stats, err = r.Run(ctx, remaining, state, false)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.UsersDeleted)

	// Soft delete disables instead. The surviving user from the hard
	// delete set (users[:1]) is still LDAP-managed, so the full
	// clear disables it too.
	users2 := desiredUsers()
	stats, err = r.Run(ctx, users2, nil, true)
	require.NoError(t, err)
	assert.Equal(t, len(users2), stats.UsersCreated)
	assert.Zero(t, stats.UsersDeleted)

	stats, err = r.Run(ctx, nil, nil, true)
	require.NoError(t, err)
	assert.Equal(t, len(users2)+1, stats.UsersDisabled)
	assert.Zero(t, stats.UsersDeleted)
}

func TestSyncGroupMembership(t *testing.T) {
	r := newReconciler(t)
	ctx := t.Context()

	users := desiredUsers()
	group := desiredGroup{
		LDAPID:  "g-" + stamp(),
		Name:    "team" + stamp(),
		Members: []string{users[0].Username},
	}

	stats, err := r.Run(ctx, users, []desiredGroup{group}, false)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.GroupsCreated)

	// Grow membership to both users; idempotence on the second run.
	group.Members = []string{users[0].Username, users[1].Username}
	stats, err = r.Run(ctx, users, []desiredGroup{group}, false)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.GroupsUpdated)

	stats, err = r.Run(ctx, users, []desiredGroup{group}, false)
	require.NoError(t, err)
	assert.Zero(t, stats.GroupsCreated)
	assert.Zero(t, stats.GroupsDeleted)
}

func TestSyncDeletesMissingGroup(t *testing.T) {
	r := newReconciler(t)
	ctx := t.Context()

	group := desiredGroup{LDAPID: "g-" + stamp(), Name: "gone" + stamp()}
	_, err := r.Run(ctx, nil, []desiredGroup{group}, false)
	require.NoError(t, err)

	stats, err := r.Run(ctx, nil, nil, false)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.GroupsDeleted)
}
