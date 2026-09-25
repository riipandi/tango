package jwtutils

import (
	"context"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keySource is a SigningKeySource over one HMAC secret, the one-stack
// deployment the tests sign with.
type keySource struct {
	secret    string
	algorithm jwa.SignatureAlgorithm
}

func (k keySource) hmacKey() (jwk.Key, error) {
	raw := []byte(k.secret)
	if len(raw) < 32 {
		padded := make([]byte, 32)
		for i := range padded {
			padded[i] = k.secret[i%len(k.secret)]
		}
		raw = padded
	}
	return jwk.Import(raw)
}

func (k keySource) SignKey(context.Context) (jwk.Key, error) {
	return k.hmacKey()
}

func (k keySource) VerifyKeySet(context.Context) (jwk.Set, error) {
	return jwk.NewSet(), nil
}

func (k keySource) HMACKey(context.Context) (jwk.Key, error) {
	return k.hmacKey()
}

func (k keySource) SigningAlgorithm() (jwa.SignatureAlgorithm, error) {
	return k.algorithm, nil
}

func TestAccessVerifierRoundTripsTheClaims(t *testing.T) {
	source := keySource{secret: "0123456789abcdeffedcba9876543210", algorithm: jwa.HS256()}
	signer, err := NewSigner[AccessClaims](mustHMACKey(t, source.secret), jwa.HS256())
	require.NoError(t, err)
	signer = signer.WithIssuer("https://tango.example").WithTTL(time.Hour)

	token, err := signer.Sign(AccessClaims{
		Email:     "ada@example.com",
		Username:  "ada",
		IsAdmin:   true,
		SessionID: "sess_01abc",
	}, Standard{Subject: "0197abc", IssuedAt: time.Now()})
	require.NoError(t, err)

	verified, err := NewAccessVerifier(source, "https://tango.example").Verify(t.Context(), token)
	require.NoError(t, err)

	assert.Equal(t, "0197abc", verified.Subject)
	assert.Equal(t, "ada@example.com", verified.Private.Email)
	assert.Equal(t, "ada", verified.Private.Username)
	assert.True(t, verified.Private.IsAdmin)
	assert.Equal(t, "sess_01abc", verified.Private.SessionID)
}

func TestAccessVerifierRejectsAnotherIssuer(t *testing.T) {
	source := keySource{secret: "0123456789abcdeffedcba9876543210", algorithm: jwa.HS256()}
	signer, err := NewSigner[AccessClaims](mustHMACKey(t, source.secret), jwa.HS256())
	require.NoError(t, err)

	token, err := signer.Sign(AccessClaims{}, Standard{Subject: "0197abc", IssuedAt: time.Now()})
	require.NoError(t, err)

	_, err = NewAccessVerifier(source, "https://tango.example").Verify(t.Context(), token)
	assert.Error(t, err)
}

// TestAccessVerifierPinsTheAlgorithm pins the algorithm defense: the
// verifier is built with the exact algorithm the key material resolves to, so
// a token signed under another algorithm with the same secret is refused
// before any key is tried — the confusion the open verifier note warned of.
func TestAccessVerifierPinsTheAlgorithm(t *testing.T) {
	// The secret is 64 bytes so it satisfies the HS384 minimum the offending
	// signer demands; the pinning is what the assertion is about.
	secret := "0123456789abcdeffedcba98765432100123456789abcdeffedcba9876543210"
	source := keySource{secret: secret, algorithm: jwa.HS256()}

	signer, err := NewSigner[AccessClaims](mustHMACKey(t, secret), jwa.HS384())
	require.NoError(t, err)
	token, err := signer.Sign(AccessClaims{}, Standard{IssuedAt: time.Now()})
	require.NoError(t, err)

	_, err = NewAccessVerifier(source, "https://tango.example").Verify(t.Context(), token)
	assert.Error(t, err, "an HS384 token must not verify under an HS256-only verifier")
}

func mustHMACKey(t *testing.T, secret string) jwk.Key {
	t.Helper()
	key, err := keySource{secret: secret}.hmacKey()
	require.NoError(t, err)
	return key
}
