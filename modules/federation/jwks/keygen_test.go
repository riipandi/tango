package jwks

import (
	"crypto/ecdsa"
	"crypto/rsa"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeyRS256(t *testing.T) {
	key, err := GenerateKey(RS256)
	require.NoError(t, err)

	assert.Equal(t, "jwk", key.ID.Prefix())
	assert.Equal(t, key.ID.String(), key.KeyID)
	assert.Equal(t, KeyTypeRSA, key.KeyType)
	assert.NotEmpty(t, key.PublicPEM)
	assert.NotEmpty(t, key.PrivatePEM)

	raw, err := parsePrivatePEM(key.PrivatePEM)
	require.NoError(t, err)
	_, isRSA := raw.(*rsa.PrivateKey)
	assert.True(t, isRSA, "private half should decode to an RSA key")
}

func TestGenerateKeyES256(t *testing.T) {
	key, err := GenerateKey(ES256)
	require.NoError(t, err)

	assert.Equal(t, KeyTypeEC, key.KeyType)
	raw, err := parsePrivatePEM(key.PrivatePEM)
	require.NoError(t, err)
	_, ok := raw.(*ecdsa.PrivateKey)
	assert.True(t, ok, "private half should decode to an EC key")
}

func TestGenerateKeyUniqueKids(t *testing.T) {
	first, err := GenerateKey(RS256)
	require.NoError(t, err)
	second, err := GenerateKey(RS256)
	require.NoError(t, err)
	assert.NotEqual(t, first.KeyID, second.KeyID)
}

func TestGenerateKeyUnsupportedAlgorithm(t *testing.T) {
	_, err := GenerateKey("HS256")
	assert.ErrorContains(t, err, "unsupported signing algorithm")
}

func TestBindKeyCarriesKidAndAlg(t *testing.T) {
	key, err := GenerateKey(ES256)
	require.NoError(t, err)

	raw, err := parsePublicPEM(key.PublicPEM)
	require.NoError(t, err)
	bound, err := bindKey(raw, key.KeyID, ES256)
	require.NoError(t, err)

	var kid string
	require.NoError(t, bound.Get(jwk.KeyIDKey, &kid))
	assert.Equal(t, key.KeyID, kid)

	var alg jwa.SignatureAlgorithm
	require.NoError(t, bound.Get(jwk.AlgorithmKey, &alg))
	assert.Equal(t, jwa.ES256().String(), alg.String())
}
