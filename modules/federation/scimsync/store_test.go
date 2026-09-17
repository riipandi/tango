package scimsync

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// newStore builds the provider store over the shared test container
// with a real cipher (tokens must round-trip encrypted). The fixture
// client ID is stored in clientFixtureID; the raw datastore comes back
// for tests that need extra fixtures.
func newStore(t *testing.T) (*PostgresStore, *datastore.Postgres) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	cipher, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	clientFixtureID = "oidc_client_" + stamp[len(stamp)-8:]
	fixtureDS = ds
	if _, err := ds.Exec(ctx,
		"INSERT INTO public.oidc_clients (id, name) VALUES ($1, $2)", clientFixtureID, "scim-test"); err != nil {
		t.Fatalf("insert client fixture: %v", err)
	}
	return NewPostgresStore(ds, cipher), ds
}

var clientFixtureID string

// fixtureDS is the datastore of the most recent newStore call; helper
// fixtures insert through it.
var fixtureDS *datastore.Postgres

// newClientFixture inserts another oidc_clients row and returns its
// id (the store refuses a second provider for the same client).
func newClientFixture(t *testing.T, ds *datastore.Postgres, name string) string {
	t.Helper()
	id := "oidc_client_" + strings.ToLower(name) + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)[:8]
	if _, err := ds.Exec(t.Context(),
		"INSERT INTO public.oidc_clients (id, name) VALUES ($1, $2)", id, name); err != nil {
		t.Fatalf("insert client fixture %s: %v", name, err)
	}
	return id
}

func TestProviderLifecycle(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	params := UpsertParams{
		Endpoint:     "https://scim.example.com/v2",
		Token:        "secret-token-" + strconv.Itoa(int(time.Now().UnixNano())),
		OIDCClientID: clientFixtureID,
	}
	created, err := store.Create(ctx, params)
	require.NoError(t, err)
	assert.False(t, created.ID.IsZero())
	assert.Equal(t, params.Endpoint, created.Endpoint)

	// Token round-trips decrypted.
	got, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, params.Token, got.Token)

	// Duplicate for the same client is rejected.
	_, err = store.Create(ctx, params)
	assert.ErrorIs(t, err, ErrDuplicate)

	// Unknown client is rejected.
	_, err = store.Create(ctx, UpsertParams{Endpoint: params.Endpoint, Token: "x", OIDCClientID: "missing"})
	assert.ErrorIs(t, err, ErrUnknownClient)

	// Update replaces endpoint and token.
	updated, err := store.Update(ctx, created.ID, UpsertParams{
		Endpoint:     "https://scim2.example.com/v2",
		Token:        "rotated",
		OIDCClientID: params.OIDCClientID,
	})
	require.NoError(t, err)
	assert.Equal(t, "https://scim2.example.com/v2", updated.Endpoint)

	got, err = store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, "rotated", got.Token)

	// MarkSynced stamps the timestamp.
	before := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, store.MarkSynced(ctx, created.ID, before))
	got, err = store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.NotNil(t, got.LastSyncedAt)
	assert.WithinDuration(t, before, *got.LastSyncedAt, time.Second)

	// Delete removes the row.
	require.NoError(t, store.Delete(ctx, created.ID))
	_, err = store.GetByID(ctx, created.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestProviderGetByClient(t *testing.T) {
	store, _ := newStore(t)
	ctx := t.Context()

	_, err := store.GetByClient(ctx, clientFixtureID)
	assert.ErrorIs(t, err, ErrNotFound)

	params := UpsertParams{Endpoint: "https://s.example.com", Token: "t", OIDCClientID: clientFixtureID}
	created, err := store.Create(ctx, params)
	require.NoError(t, err)

	got, err := store.GetByClient(ctx, clientFixtureID)
	require.NoError(t, err)
	assert.Equal(t, created.ID.String(), got.ID.String())
}

// TestProviderSealedAtRest verifies the enc: marker lands in the
// raw row and that an undecryptable token (wrong key, tampering)
// fails closed instead of passing ciphertext or plaintext through.
func TestProviderSealedAtRest(t *testing.T) {
	store, ds := newStore(t)
	ctx := t.Context()

	params := UpsertParams{
		Endpoint:     "https://scim.example.com/v2",
		Token:        "secret-token-" + strconv.Itoa(int(time.Now().UnixNano())),
		OIDCClientID: clientFixtureID,
	}
	created, err := store.Create(ctx, params)
	require.NoError(t, err)

	var stored string
	require.NoError(t, ds.QueryRow(ctx,
		"SELECT token FROM public.scim_service_providers WHERE id = $1", created.ID.UUIDBytes()).Scan(&stored))
	assert.True(t, strings.HasPrefix(stored, "enc:"),
		"stored token must carry the enc: marker, got %q", stored)

	// Tamper with the ciphertext: reads fail closed, the tampered
	// value is never handed out as plaintext.
	_, err = ds.Exec(ctx,
		"UPDATE public.scim_service_providers SET token = 'enc:AAAA' WHERE id = $1", created.ID.UUIDBytes())
	require.NoError(t, err)

	_, err = store.GetByID(ctx, created.ID)
	assert.Error(t, err, "tampered token must not decrypt")

	_, err = store.List(ctx)
	assert.Error(t, err, "one tampered row fails the whole listing")
}
