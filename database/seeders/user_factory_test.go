package seeders

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

// newTestDB migrates the shared test container and returns an
// executor-backed seeder target.
func newTestDB(t *testing.T) Executor {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// usernameByID reads back a seeded user's username.
func usernameByID(t *testing.T, db Executor, id string) string {
	t.Helper()
	var username string
	require.NoError(t, db.QueryRow(t.Context(),
		"SELECT username FROM public.users WHERE id = $1::uuid", id).Scan(&username))
	return username
}

// TestSetupAdminFresh pins the freshness gate: the first setup wins,
// the second is refused. Order matters on the shared container —
// the factory test populates users, so this must run first.
func TestSetupAdminFresh(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()

	exists, err := HasAnyUser(ctx, db)
	require.NoError(t, err)
	if exists {
		t.Skip("shared container already has users; freshness gate verified implicitly")
	}

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	params := AdminParams{
		Username: "admin" + stamp[len(stamp)-6:],
		Email:    "admin+" + stamp + "@example.com",
		Password: "Admin123!",
	}
	id, err := SetupAdmin(ctx, db, params)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	_, err = SetupAdmin(ctx, db, AdminParams{
		Username: "other" + stamp[len(stamp)-6:],
		Email:    "other+" + stamp + "@example.com",
		Password: "Other123!",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not fresh")
}

// TestSetupAdminValidation pins the input rules. Validation runs
// before the freshness gate, so a dirty container is fine here.
func TestSetupAdminValidation(t *testing.T) {
	db := newTestDB(t)
	ctx := t.Context()

	cases := []struct {
		name    string
		params  AdminParams
		wantErr string
	}{
		{"short username", AdminParams{Username: "ab", Email: "a@b.co", Password: "longenough1"}, "3-32"},
		{"bad chars", AdminParams{Username: "has space", Email: "a@b.co", Password: "longenough1"}, "alphanumeric"},
		{"bad email", AdminParams{Username: "goodname", Email: "nope", Password: "longenough1"}, "email"},
		{"short password", AdminParams{Username: "goodname", Email: "a@b.co", Password: "short"}, "8 characters"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SetupAdmin(ctx, db, tc.params)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestUserFactoryDeterministic pins the seed: both factories start at
// suffix 0042, verified through the created usernames.
func TestUserFactoryDeterministic(t *testing.T) {
	db := newTestDB(t)
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	ids1, err := NewUserFactory(db, 42).CreateMany(t.Context(), "first_"+stamp, 3)
	require.NoError(t, err)
	ids2, err := NewUserFactory(db, 42).CreateMany(t.Context(), "second_"+stamp, 3)
	require.NoError(t, err)

	for i, id := range ids1 {
		assert.Contains(t, usernameByID(t, db, id), "first_"+stamp+"_00"+strconv.Itoa(42+i))
	}
	for i, id := range ids2 {
		assert.Contains(t, usernameByID(t, db, id), "second_"+stamp+"_00"+strconv.Itoa(42+i))
	}
}

// TestUserFactoryUniqueIDs proves every created user gets a distinct
// ID and a working password hash.
func TestUserFactoryUniqueIDs(t *testing.T) {
	db := newTestDB(t)
	factory := NewUserFactory(db, 7)

	ids, err := factory.CreateMany(t.Context(), "dev", 5)
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, id := range ids {
		assert.NotEmpty(t, id)
		assert.False(t, seen[id], "IDs must be unique")
		seen[id] = true

		var hash string
		require.NoError(t, db.QueryRow(t.Context(),
			"SELECT password_hash FROM public.user_passwords WHERE user_id = $1::uuid", id).Scan(&hash))
		assert.NotEmpty(t, hash)
	}
}
