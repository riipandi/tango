package seeders

import (
	"testing"

	"github.com/riipandi/tango/modules/identity/user"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserFactoryDeterministic pins the seed, so the same names
// come out on every run — snapshot-safe fixtures.
func TestUserFactoryDeterministic(t *testing.T) {
	build := func() []string {
		factory := NewUserFactory(user.NewService(user.NewMemoryStore(), nil), 42)
		users, err := factory.CreateMany(t.Context(), "tester", 3)
		require.NoError(t, err)
		names := make([]string, 0, len(users))
		for _, u := range users {
			names = append(names, u.Name)
		}
		return names
	}

	first, second := build(), build()
	require.Len(t, first, 3)
	assert.Equal(t, first, second, "same seed must produce the same names")
	for _, name := range first {
		assert.Contains(t, name, "tester-")
	}
}

// TestUserFactoryUniqueIDs proves the service path: every created
// user gets a distinct ID and is retrievable.
func TestUserFactoryUniqueIDs(t *testing.T) {
	svc := user.NewService(user.NewMemoryStore(), nil)
	factory := NewUserFactory(svc, 7)

	users, err := factory.CreateMany(t.Context(), "dev", 5)
	require.NoError(t, err)

	seen := map[string]bool{}
	for _, u := range users {
		assert.NotEmpty(t, u.ID)
		assert.False(t, seen[u.ID], "IDs must be unique")
		seen[u.ID] = true

		stored, err := svc.GetByID(t.Context(), u.ID)
		require.NoError(t, err)
		assert.Equal(t, u.Name, stored.Name)
	}
}
