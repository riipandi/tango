package jwks

import (
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCipher is deterministic; sealed values are never reused
// across runs.
func testCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	cipher, err := crypto.NewCipher([]byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	return cipher
}

func newTestStack(t *testing.T) (*Service, Store) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	store := NewPostgresStore(ds)
	return NewService(store, testCipher(t), RS256), store
}

// kidsInSet collects the kids of a key set.
func kidsInSet(t *testing.T, set jwk.Set) []string {
	t.Helper()
	kids := make([]string, 0, set.Len())
	for i := range set.Len() {
		key, ok := set.Key(i)
		require.True(t, ok)
		kids = append(kids, kidOf(t, key))
	}
	return kids
}

func kidOf(t *testing.T, key jwk.Key) string {
	t.Helper()
	var kid string
	require.NoError(t, key.Get(jwk.KeyIDKey, &kid), "key must carry a kid")
	return kid
}

func TestStartGeneratesSigningKeyOnce(t *testing.T) {
	svc, store := newTestStack(t)
	ctx := t.Context()

	require.NoError(t, svc.Start(ctx))

	active, err := store.ActiveSigningKey(ctx)
	require.NoError(t, err)
	assert.True(t, active.IsActive)

	first := active.KeyID
	require.NoError(t, svc.Start(ctx))
	active, err = store.ActiveSigningKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, first, active.KeyID, "second start must keep the existing key")
}

func TestSignKeyAndVerifyKeySet(t *testing.T) {
	svc, _ := newTestStack(t)
	ctx := t.Context()
	require.NoError(t, svc.Start(ctx))

	signKey, err := svc.SignKey(ctx)
	require.NoError(t, err)
	assert.Equal(t, "RSA", signKey.KeyType().String())
	set, err := svc.VerifyKeySet(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{kidOf(t, signKey)}, kidsInSet(t, set))
}

func TestRotateKeepsOverlapPublished(t *testing.T) {
	svc, _ := newTestStack(t)
	ctx := t.Context()
	require.NoError(t, svc.Start(ctx))

	before, err := svc.SignKey(ctx)
	require.NoError(t, err)
	oldKid := kidOf(t, before)

	require.NoError(t, svc.Rotate(ctx))

	// The fresh key signs; both keys stay published during overlap.
	after, err := svc.SignKey(ctx)
	require.NoError(t, err)
	newKid := kidOf(t, after)
	assert.NotEqual(t, oldKid, newKid)

	set, err := svc.VerifyKeySet(ctx)
	require.NoError(t, err)
	kids := kidsInSet(t, set)
	assert.Len(t, kids, 2, "overlap window publishes retired + new key")
	assert.Contains(t, kids, oldKid)
	assert.Contains(t, kids, newKid)
}

// TestSignVerifyRoundTrip proves the provider wiring end to end: a
// token minted from SignKey must verify against the published JWKS
// (kid selection, public halves only).
func TestSignVerifyRoundTrip(t *testing.T) {
	svc, _ := newTestStack(t)
	ctx := t.Context()
	require.NoError(t, svc.Start(ctx))

	signKey, err := svc.SignKey(ctx)
	require.NoError(t, err)
	alg, ok := signKey.Algorithm()
	require.True(t, ok)

	signer, err := jwtutils.NewSigner[map[string]any](signKey, alg.(jwa.SignatureAlgorithm))
	require.NoError(t, err)
	token, err := signer.Sign(map[string]any{"scope": "read"}, jwtutils.Standard{Subject: "user_1"})
	require.NoError(t, err)

	set, err := svc.VerifyKeySet(ctx)
	require.NoError(t, err)
	verifier, err := jwtutils.NewVerifier[map[string]any](nil, alg.(jwa.SignatureAlgorithm))
	require.NoError(t, err)
	verified, err := verifier.WithKeySet(set).Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user_1", verified.Subject)
	assert.Equal(t, "read", verified.Private["scope"])
}
