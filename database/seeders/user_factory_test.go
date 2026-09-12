package seeders

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestService builds the user service on the real Postgres
// store over the shared test container.
func newTestService(t *testing.T) *user.Service {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	return user.NewService(user.NewPostgresStore(store), nil)
}

// TestUserFactoryDeterministic pins the seed: the suffix sequence
// is identical across runs (rows persist against a shared database,
// so each build uses its own unique base name).
func TestUserFactoryDeterministic(t *testing.T) {
	svc := newTestService(t)
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	suffixes := func(base string) []string {
		factory := NewUserFactory(svc, 42)
		users, err := factory.CreateMany(t.Context(), base, 3)
		require.NoError(t, err)
		out := make([]string, 0, len(users))
		for _, u := range users {
			_, suffix, ok := strings.Cut(u.Username, base+"_")
			require.True(t, ok, u.Username)
			out = append(out, suffix)
		}
		return out
	}

	first := suffixes("first_" + stamp)
	second := suffixes("second_" + stamp)

	require.Len(t, first, 3)
	assert.Equal(t, first, second, "same seed must produce the same suffix sequence")
	assert.Equal(t, []string{"0042", "0043", "0044"}, first)
}

// TestUserFactoryUniqueIDs proves the service path: every created
// user gets a distinct ID and is retrievable.
func TestUserFactoryUniqueIDs(t *testing.T) {
	svc := newTestService(t)
	factory := NewUserFactory(svc, 7)

	users, err := factory.CreateMany(t.Context(), "dev", 5)
	require.NoError(t, err)

	seen := map[identity.UserID]bool{}
	for _, u := range users {
		assert.NotEmpty(t, u.ID)
		assert.False(t, seen[u.ID], "IDs must be unique")
		seen[u.ID] = true

		stored, err := svc.GetByID(t.Context(), u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.Username, stored.Username)
	}
}
