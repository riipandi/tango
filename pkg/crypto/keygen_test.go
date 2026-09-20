package crypto

import (
	"encoding/base64"
	"testing"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeyGeneratorDefaults(t *testing.T) {
	generator, err := NewKeyGenerator("")
	require.NoError(t, err)

	keyPair, secret := generator.Algorithms()
	assert.Equal(t, DefaultSignatureAlgorithm, keyPair)
	assert.Equal(t, DefaultSecretAlgorithm, secret)
}

func TestNewKeyGeneratorSelectsRole(t *testing.T) {
	es384, err := NewKeyGenerator("ES384")
	require.NoError(t, err)
	keyPair, secret := es384.Algorithms()
	assert.Equal(t, "ES384", keyPair, "an asymmetric algorithm replaces the key pair")
	assert.Equal(t, DefaultSecretAlgorithm, secret, "the HMAC secret keeps its default")

	hs512, err := NewKeyGenerator("HS512")
	require.NoError(t, err)
	keyPair, secret = hs512.Algorithms()
	assert.Equal(t, DefaultSignatureAlgorithm, keyPair, "the key pair keeps its default")
	assert.Equal(t, "HS512", secret, "an HS* algorithm replaces the secret")
}

func TestNewKeyGeneratorRejectsUnsupportedAlgorithms(t *testing.T) {
	for _, algorithm := range []string{"HS999", "none", "ES999"} {
		_, err := NewKeyGenerator(algorithm)
		assert.ErrorIs(t, err, ErrUnsupportedAlgorithm, algorithm)
	}
}

func TestGenerateEmitsAllFourKeys(t *testing.T) {
	for _, algorithm := range []string{"", "ES256", "ES384", "ES512", "EdDSA", "RS256", "PS256", "HS256", "HS384", "HS512"} {
		generator, err := NewKeyGenerator(algorithm)
		require.NoError(t, err)

		keys, err := generator.Generate()
		require.NoError(t, err)

		assert.Equal(t, []string{EnvAppSecretKey, EnvAuthPrivateKey, EnvAuthPublicKey, EnvAuthSecretKey},
			keys.Names(), algorithm)
		assert.Len(t, keys[EnvAppSecretKey], KeyHexLength, algorithm)
	}
}

func TestGenerateKeyPair(t *testing.T) {
	generator, err := NewKeyGenerator("ES256")
	require.NoError(t, err)

	keys, err := generator.Generate()
	require.NoError(t, err)

	_, err = ParseKeyHex(keys[EnvAppSecretKey])
	require.NoError(t, err)

	private := decodeJWK(t, keys[EnvAuthPrivateKey])
	public := decodeJWK(t, keys[EnvAuthPublicKey])

	require.NoError(t, private.Validate())
	require.NoError(t, public.Validate())

	privateKID, ok := private.KeyID()
	require.True(t, ok)
	publicKID, ok := public.KeyID()
	require.True(t, ok)
	assert.Equal(t, privateKID, publicKID, "both halves must share the kid")

	alg, ok := public.Algorithm()
	require.True(t, ok)
	assert.Equal(t, "ES256", alg.String())

	assert.True(t, private.Has(jwk.ECDSADKey), "private JWK must carry the EC private scalar")
	assert.False(t, public.Has(jwk.ECDSADKey), "public JWK must not leak the private scalar")
}

func TestGenerateHMACSecretSize(t *testing.T) {
	for algorithm, want := range map[string]int{"HS256": 64, "HS384": 96, "HS512": 128} {
		generator, err := NewKeyGenerator(algorithm)
		require.NoError(t, err)

		keys, err := generator.Generate()
		require.NoError(t, err)
		assert.Len(t, keys[EnvAuthSecretKey], want, algorithm)
	}
}

func TestGeneratedKeysAreUnique(t *testing.T) {
	generator, err := NewKeyGenerator("ES256")
	require.NoError(t, err)

	first, err := generator.Generate()
	require.NoError(t, err)
	second, err := generator.Generate()
	require.NoError(t, err)

	assert.NotEqual(t, first[EnvAppSecretKey], second[EnvAppSecretKey])
	assert.NotEqual(t, first[EnvAuthPrivateKey], second[EnvAuthPrivateKey])
	assert.NotEqual(t, first[EnvAuthSecretKey], second[EnvAuthSecretKey])
}

func TestGeneratedKeysSignAndVerify(t *testing.T) {
	generator, err := NewKeyGenerator("ES256")
	require.NoError(t, err)
	keys, err := generator.Generate()
	require.NoError(t, err)

	private := decodeJWK(t, keys[EnvAuthPrivateKey])
	signer, err := jwtutils.NewSigner[struct{}](private, jwa.ES256())
	require.NoError(t, err)

	token, err := signer.Sign(struct{}{}, jwtutils.Standard{Subject: "user_123"})
	require.NoError(t, err)

	public := decodeJWK(t, keys[EnvAuthPublicKey])
	verifier, err := jwtutils.NewVerifier[struct{}](public, jwa.ES256())
	require.NoError(t, err)

	verified, err := verifier.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "user_123", verified.Subject)
}

// decodeJWK decodes a base64-encoded JWK value.
func decodeJWK(t *testing.T, encoded string) jwk.Key {
	t.Helper()
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	require.NoError(t, err)

	key, err := jwk.ParseKey(raw)
	require.NoError(t, err)
	return key
}
