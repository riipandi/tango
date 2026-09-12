package user

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore builds a Postgres-backed store over the shared test
// container with all migrations applied. The container is shared
// per test binary; data is isolated by unique usernames.
func newTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	store, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	return NewPostgresStore(store)
}

func TestPostgresStoreCreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	created, err := store.Create(ctx, CreateParams{
		Username:    "pguser_" + stamp,
		Email:       "pguser-" + stamp + "@example.com",
		FirstName:   "First",
		LastName:    "Last",
		DisplayName: "PG User",
	})
	require.NoError(t, err)

	assert.Equal(t, "user", created.ID.Prefix())
	assert.Equal(t, "PG User", created.DisplayName)
	require.NotNil(t, created.FirstName)
	assert.Equal(t, "First", *created.FirstName)
	assert.False(t, created.CreatedAt.IsZero())
	assert.Nil(t, created.UpdatedAt)

	fetched, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.Username, fetched.Username)
	assert.Equal(t, created.Email, fetched.Email)
	require.NotNil(t, fetched.LastName)
	assert.Equal(t, "Last", *fetched.LastName)
	assert.False(t, fetched.CreatedAt.IsZero())
}

func TestPostgresStoreNullables(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	created, err := store.Create(ctx, CreateParams{
		Username: "nonames_" + stamp,
		Email:    "nonames-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	fetched, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Nil(t, fetched.FirstName)
	assert.Nil(t, fetched.LastName)
	assert.Nil(t, fetched.AvatarURL)
	assert.Nil(t, fetched.Locale)
	assert.True(t, strings.HasPrefix(fetched.DisplayName, "nonames"),
		"display name falls back to the email local part: %q", fetched.DisplayName)
}

func TestPostgresStoreErrors(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	_, err := store.Create(ctx, CreateParams{
		Username: "dupuser_" + stamp,
		Email:    "dupuser-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	// Same username → unique violation → ErrDuplicate.
	_, err = store.Create(ctx, CreateParams{
		Username: "dupuser_" + stamp,
		Email:    "other-" + stamp + "@example.com",
	})
	assert.ErrorIs(t, err, ErrDuplicate)

	// Unknown ID → ErrNotFound.
	_, err = store.GetByID(ctx, identity.NewID[UserID]())
	assert.ErrorIs(t, err, ErrNotFound)

	// A CHECK-violating email (bypassing service validation via raw
	// SQL) maps to the domain validation error.
	_, rawErr := store.exec.Exec(ctx,
		`INSERT INTO `+usersTable+` (username, email, display_name) VALUES ($1, $2, $3)`,
		"rawbadmail_"+stamp, "not-an-email", "raw")
	require.Error(t, rawErr)
	assert.ErrorIs(t, mapStoreError(rawErr), ErrInvalidEmail)
}

func TestPostgresStoreListNewestFirst(t *testing.T) {
	store := newTestStore(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	first, err := store.Create(ctx, CreateParams{
		Username: "list1_" + stamp, Email: "list1-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	second, err := store.Create(ctx, CreateParams{
		Username: "list2_" + stamp, Email: "list2-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	users := store.List(ctx)
	require.GreaterOrEqual(t, len(users), 2)
	// The two just-created users are the newest; newest first.
	assert.Equal(t, second.ID, users[0].ID)
	assert.Equal(t, first.ID, users[1].ID)
}
