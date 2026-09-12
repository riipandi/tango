package password

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the credential store over the shared test
// container with all migrations applied.
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

func createUser(t *testing.T, u *user.PostgresStore, name string) user.User {
	t.Helper()
	created, err := u.Create(t.Context(), user.CreateParams{
		Username: name + "_" + strconv.FormatInt(time.Now().UnixNano(), 10),
		Email:    name + "-" + strconv.FormatInt(time.Now().UnixNano(), 10) + "@example.com",
	})
	require.NoError(t, err)
	return created
}

func TestUpsertAndHashByUserID(t *testing.T) {
	store, users := newTestStack(t)
	u := createUser(t, users, "pwdup")

	hasher := crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt)
	first, err := hasher.Hash("correct horse battery")
	require.NoError(t, err)
	require.NoError(t, store.Upsert(t.Context(), u.ID, first))

	// Rotate: the row updates, the hash changes.
	second, err := hasher.Hash("staple detonator")
	require.NoError(t, err)
	require.NoError(t, store.Upsert(t.Context(), u.ID, second))

	got, err := store.HashByUserID(t.Context(), u.ID)
	require.NoError(t, err)
	assert.Equal(t, second, got)

	ok, err := hasher.Verify("staple detonator", got)
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestHashByIdentity(t *testing.T) {
	store, users := newTestStack(t)
	u := createUser(t, users, "pwdid")

	hasher := crypto.NewPasswordHasher()
	hash, err := hasher.Hash("hunter2hunter2")
	require.NoError(t, err)
	require.NoError(t, store.Upsert(t.Context(), u.ID, hash))

	// Username, email, and case-folded username all resolve.
	for _, identity := range []string{u.Username, u.Email, upper(u.Username)} {
		got, found, err2 := store.HashByIdentity(t.Context(), identity)
		require.NoError(t, err2, identity)
		assert.Equal(t, hash, got, identity)
		assert.Equal(t, u.ID, found.ID, identity)
	}

	// Unknown identity never reveals whether the account exists.
	_, _, err = store.HashByIdentity(t.Context(), "ghost@example.com")
	assert.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestHashByUserIDNoRow(t *testing.T) {
	store, users := newTestStack(t)
	u := createUser(t, users, "pwdno")

	_, err := store.HashByUserID(t.Context(), u.ID)
	assert.ErrorIs(t, err, ErrNoPassword)
}

func upper(s string) string {
	b := []byte(s)
	if len(b) > 0 {
		b[0] -= 32
	}
	return string(b)
}
