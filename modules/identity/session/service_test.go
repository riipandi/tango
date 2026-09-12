package session

import (
	"strconv"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStack builds the session service over the shared test
// container: real Postgres store, real password verifier.
func newTestStack(t *testing.T, opts ...ServiceOption) (*Service, *password.Service, user.Store) {
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
	passwords := password.NewService(password.NewPostgresStore(ds),
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt), nil)
	sessions := NewService(NewPostgresStore(ds), passwords, users, nil, opts...)
	return sessions, passwords, users
}

// newUser provisions an account with a known credential.
func newUser(t *testing.T, users user.Store, passwords *password.Service, name string) user.User {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: name + "_" + stamp,
		Email:    name + "-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))
	return u
}

func TestSignInAndResolve(t *testing.T) {
	sessions, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "si")
	ctx := t.Context()

	token, _, issued, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{UserAgent: "test/1", IPAddress: "10.0.0.1"})
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.Equal(t, "password", issued.Provider)
	assert.WithinDuration(t, time.Now().Add(defaultLifetime), issued.ExpiresAt, time.Minute)

	resolvedUser, resolved, err := sessions.Resolve(ctx, token)
	require.NoError(t, err)
	assert.Equal(t, u.ID, resolvedUser.ID)
	assert.Equal(t, issued.ID, resolved.ID)
	require.NotNil(t, resolved.UserAgent)
	assert.Equal(t, "test/1", *resolved.UserAgent)

	// Unknown tokens never resolve.
	_, _, err = sessions.Resolve(ctx, "bogus-token")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRevokeCurrent(t *testing.T) {
	sessions, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "rv")
	ctx := t.Context()

	token, _, _, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{})
	require.NoError(t, err)

	require.NoError(t, sessions.RevokeCurrent(ctx, token))

	_, _, err = sessions.Resolve(ctx, token)
	assert.ErrorIs(t, err, ErrNotFound)

	// Revoking again is idempotent from the caller's view.
	require.NoError(t, sessions.RevokeCurrent(ctx, token))
}

func TestExpiry(t *testing.T) {
	// Short lifetime: past it, the session is invisible.
	sessions, passwords, users := newTestStack(t, WithLifetime(60*time.Millisecond))
	u := newUser(t, users, passwords, "exp")
	ctx := t.Context()

	token, _, _, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{})
	require.NoError(t, err)

	time.Sleep(90 * time.Millisecond)
	_, _, err = sessions.Resolve(ctx, token)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestRevokeAllKeepsCurrent(t *testing.T) {
	sessions, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "all")
	ctx := t.Context()

	tokenA, _, _, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{})
	require.NoError(t, err)
	tokenB, _, issuedB, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{})
	require.NoError(t, err)

	require.NoError(t, sessions.RevokeAllForUser(ctx, u.ID, issuedB.ID))

	_, _, err = sessions.Resolve(ctx, tokenA)
	assert.ErrorIs(t, err, ErrNotFound)

	_, resolvedB, err := sessions.Resolve(ctx, tokenB)
	require.NoError(t, err)
	assert.Equal(t, issuedB.ID, resolvedB.ID)
}

func TestListAndRevokeForUser(t *testing.T) {
	sessions, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "ls")
	ctx := t.Context()

	_, _, first, err := sessions.SignIn(ctx, u.Username, "s3cret-p@ss", Meta{})
	require.NoError(t, err)
	_, _, _, err = sessions.SignIn(ctx, u.Email, "s3cret-p@ss", Meta{})
	require.NoError(t, err)

	list, err := sessions.ListForUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Len(t, list, 2)

	require.NoError(t, sessions.RevokeForUser(ctx, u.ID, first.ID))
	list, err = sessions.ListForUser(ctx, u.ID)
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.NotEqual(t, first.ID, list[0].ID)
}
