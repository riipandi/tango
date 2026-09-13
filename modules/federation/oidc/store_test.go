package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"strconv"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// staticKeyProvider serves one generated RSA key (the provider
// signs with RS256); the tests don't need the full jwks module.
type staticKeyProvider struct {
	key    jwk.Key
	pubSet jwk.Set
}

func newStaticKeyProvider(t *testing.T) *staticKeyProvider {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(priv)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "test-signing-key"))
	require.NoError(t, key.Set(jwk.AlgorithmKey, jwa.RS256()))

	pub, err := jwk.PublicKeyOf(key)
	require.NoError(t, err)
	pubSet := jwk.NewSet()
	require.NoError(t, pubSet.AddKey(pub))
	return &staticKeyProvider{key: key, pubSet: pubSet}
}

func (p *staticKeyProvider) SignKey(context.Context) (jwk.Key, error) { return p.key, nil }

func (p *staticKeyProvider) VerifyKeySet(context.Context) (jwk.Set, error) {
	return p.pubSet, nil
}

// testStack builds the provider over a real Postgres container.
// The shared datastore is returned for sibling-store fixtures.
func testStack(t *testing.T) (*Service, Store, datastore.Store) {
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
	return NewService(store, newStaticKeyProvider(t), "https://sso.test", "tango_session"), store, ds
}

// stamp uniquifies data on the shared container.
func stamp() string { return strconv.FormatInt(time.Now().UnixNano(), 10) }

// clientFixture registers a confidential client with one callback.
// Consent is skipped so the E2E path issues codes directly; the
// consent flow is covered by the interaction tests.
func clientFixture(ctx context.Context, t *testing.T, store Store, name string) Client {
	t.Helper()
	created, err := store.CreateClient(ctx, ClientCreateParams{
		Name:                        name,
		CallbackURLs:                []string{"https://rp.example/callback*"},
		SecretHash:                  sha256Hex("rp-secret"),
		SkipConsent:                 true,
		AccessTokenDurationMinutes:  60,
		RefreshTokenDurationMinutes: 43200,
	})
	require.NoError(t, err)
	return created
}
