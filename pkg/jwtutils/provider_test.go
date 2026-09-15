package jwtutils

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubProvider struct {
	signKey jwk.Key
	keySet  jwk.Set
	fail    bool
	loads   int
}

func newStubProvider(t *testing.T) *stubProvider {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := jwk.Import(priv)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "stub-kid"))

	set := jwk.NewSet()
	pub, err := jwk.PublicKeyOf(key)
	require.NoError(t, err)
	require.NoError(t, set.AddKey(pub))

	return &stubProvider{signKey: key, keySet: set}
}

func (s *stubProvider) SignKey(context.Context) (jwk.Key, error) {
	s.loads++
	if s.fail {
		return nil, errors.New("backend down")
	}
	return s.signKey, nil
}

func (s *stubProvider) VerifyKeySet(context.Context) (jwk.Set, error) {
	s.loads++
	if s.fail {
		return nil, errors.New("backend down")
	}
	return s.keySet, nil
}

func TestCachedKeyProviderServesWithinTTL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newStubProvider(t)
		cache := NewCachedKeyProvider(inner, time.Minute)

		for range 5 {
			key, err := cache.SignKey(context.Background())
			require.NoError(t, err)
			assert.NotNil(t, key)
			_, err = cache.VerifyKeySet(context.Background())
			require.NoError(t, err)
		}
		assert.Equal(t, 2, inner.loads, "one load per entry within the TTL")

		time.Sleep(2 * time.Minute)
		_, err := cache.SignKey(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 3, inner.loads, "TTL expiry forces a reload")
	})
}

func TestCachedKeyProviderInvalidate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newStubProvider(t)
		cache := NewCachedKeyProvider(inner, time.Hour)

		_, err := cache.SignKey(context.Background())
		require.NoError(t, err)
		cache.Invalidate()

		_, err = cache.SignKey(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, inner.loads, "invalidation drops the entry")
	})
}

func TestCachedKeyProviderDoesNotCacheFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		inner := newStubProvider(t)
		inner.fail = true
		cache := NewCachedKeyProvider(inner, time.Hour)

		_, err := cache.SignKey(context.Background())
		assert.Error(t, err)

		inner.fail = false
		_, err = cache.SignKey(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, inner.loads)
	})
}
