package jwtutils

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// accessClaims is the typed private claim set used across tests.
type accessClaims struct {
	Roles []string `json:"roles,omitempty"`
	Plan  string   `json:"plan,omitempty"`
}

// normalizingClaims overrides private-claim reconstruction via the
// optional decoder hook.
type normalizingClaims struct {
	Plan string `json:"plan"`
}

func (c *normalizingClaims) DecodePrivateClaims(params map[string]any) error {
	raw, ok := params["plan"].(string)
	if !ok {
		raw = "free"
	}
	c.Plan = strings.ToUpper(raw)
	return nil
}

func hmacKey(t *testing.T, secret string) jwk.Key {
	t.Helper()
	key, err := jwk.Import([]byte(secret))
	require.NoError(t, err)
	return key
}

func ed25519Key(t *testing.T) jwk.Key {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	_ = pub
	key, err := jwk.Import(priv)
	require.NoError(t, err)
	return key
}

func TestSignVerifyRoundTrip(t *testing.T) {
	signer, err := NewSigner[accessClaims](hmacKey(t, "secret-1"), jwa.HS256())
	require.NoError(t, err)

	token, err := signer.Sign(
		accessClaims{Roles: []string{"admin", "billing"}, Plan: "pro"},
		Standard{Issuer: "tango", Subject: "user_123", Audience: []string{"api"}, JWTID: "jti-1"},
	)
	require.NoError(t, err)
	assert.True(t, strings.Count(token, ".") == 2)

	verifier, err := NewVerifier[accessClaims](hmacKey(t, "secret-1"), jwa.HS256())
	require.NoError(t, err)

	verified, err := verifier.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "tango", verified.Issuer)
	assert.Equal(t, "user_123", verified.Subject)
	assert.Equal(t, []string{"api"}, verified.Audience)
	assert.Equal(t, "jti-1", verified.JWTID)
	assert.Equal(t, accessClaims{Roles: []string{"admin", "billing"}, Plan: "pro"}, verified.Private)
}

func TestSignVerifyEd25519(t *testing.T) {
	key := ed25519Key(t)

	signer, err := NewSigner[accessClaims](key, jwa.EdDSA())
	require.NoError(t, err)
	token, err := signer.Sign(accessClaims{Plan: "pro"}, Standard{Subject: "user_1"})
	require.NoError(t, err)

	verifier, err := NewVerifier[accessClaims](key, jwa.EdDSA())
	require.NoError(t, err)
	verified, err := verifier.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "pro", verified.Private.Plan)
}

func TestSignerAppliesDefaults(t *testing.T) {
	signer := mustSigner[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		WithIssuer("https://id.tango.test").
		WithAudience("api", "dashboard").
		WithTTL(15 * time.Minute)

	token, err := signer.Sign(accessClaims{Plan: "free"}, Standard{Subject: "user_9"})
	require.NoError(t, err)

	verified, err := mustVerifier[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		WithIssuer("https://id.tango.test").
		Verify(token)
	require.NoError(t, err)

	assert.Equal(t, "https://id.tango.test", verified.Issuer)
	assert.Equal(t, []string{"api", "dashboard"}, verified.Audience)
	assert.Equal(t, "user_9", verified.Subject)
	assert.WithinDuration(t, time.Now().Add(15*time.Minute), verified.ExpiresAt, 30*time.Second)
	assert.WithinDuration(t, time.Now(), verified.IssuedAt, 30*time.Second)
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	token, err := mustSigner[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		Sign(accessClaims{Plan: "pro"}, Standard{})
	require.NoError(t, err)

	verifier, err := NewVerifier[accessClaims](hmacKey(t, "other-secret"), jwa.HS256())
	require.NoError(t, err)

	_, err = verifier.Verify(token)
	assert.Error(t, err)
}

func TestVerifyRejectsTamperedToken(t *testing.T) {
	token, err := mustSigner[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		Sign(accessClaims{Plan: "pro"}, Standard{})
	require.NoError(t, err)

	tampered := token[:len(token)-2] + "xx"

	verifier, err := NewVerifier[accessClaims](hmacKey(t, "secret-1"), jwa.HS256())
	require.NoError(t, err)
	_, err = verifier.Verify(tampered)
	assert.Error(t, err)
}

func TestVerifyRejectsExpired(t *testing.T) {
	token, err := mustSigner[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		Sign(accessClaims{}, Standard{ExpiresAt: time.Now().Add(-time.Minute)})
	require.NoError(t, err)

	verifier, err := NewVerifier[accessClaims](hmacKey(t, "secret-1"), jwa.HS256())
	require.NoError(t, err)
	_, err = verifier.Verify(token)
	assert.Error(t, err, "expired tokens must not verify")
}

func TestVerifyEnforcesIssuerAndAudience(t *testing.T) {
	token, err := mustSigner[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		Sign(accessClaims{}, Standard{Issuer: "other-issuer", Audience: []string{"other-api"}})
	require.NoError(t, err)

	_, err = mustVerifier[accessClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		WithIssuer("tango-issuer").
		WithAudience("api").
		Verify(token)
	assert.Error(t, err)
}

func TestVerifyWithKeySetSelectsKid(t *testing.T) {
	signingKey := hmacKey(t, "secret-1")
	require.NoError(t, jwk.AssignKeyID(signingKey))
	// Key-set matching filters candidates by the key's alg field.
	require.NoError(t, signingKey.Set(jwk.AlgorithmKey, "HS256"))

	keySet := jwk.NewSet()
	require.NoError(t, keySet.AddKey(signingKey))
	require.NoError(t, keySet.AddKey(hmacKey(t, "unused-secret")))

	token, err := mustSigner[accessClaims](t, signingKey, jwa.HS256()).
		Sign(accessClaims{Plan: "pro"}, Standard{})
	require.NoError(t, err)

	verifier, err := NewVerifier[accessClaims](nil, jwa.HS256())
	require.NoError(t, err)
	verified, err := verifier.WithKeySet(keySet).Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "pro", verified.Private.Plan)
}

func TestVerifyWithEmptyKeySetFails(t *testing.T) {
	verifier, err := NewVerifier[accessClaims](nil, jwa.HS256())
	require.NoError(t, err)

	_, err = verifier.WithKeySet(jwk.NewSet()).Verify("a.b.c")
	assert.ErrorIs(t, err, ErrMissingKeySet)
}

func TestOptionalPrivateClaimDecoder(t *testing.T) {
	token, err := mustSigner[normalizingClaims](t, hmacKey(t, "secret-1"), jwa.HS256()).
		Sign(normalizingClaims{Plan: "pro"}, Standard{})
	require.NoError(t, err)

	verifier, err := NewVerifier[normalizingClaims](hmacKey(t, "secret-1"), jwa.HS256())
	require.NoError(t, err)

	verified, err := verifier.Verify(token)
	require.NoError(t, err)
	assert.Equal(t, "PRO", verified.Private.Plan, "decoder hook must own reconstruction")
}

func TestSignerRejectsMissingKey(t *testing.T) {
	_, err := NewSigner[accessClaims](nil, jwa.HS256())
	assert.ErrorIs(t, err, ErrMissingKey)
}

func mustSigner[T any](t *testing.T, key jwk.Key, algorithm jwa.SignatureAlgorithm) *Signer[T] {
	t.Helper()
	signer, err := NewSigner[T](key, algorithm)
	require.NoError(t, err)
	return signer
}

func mustVerifier[T any](t *testing.T, key jwk.Key, algorithm jwa.SignatureAlgorithm) *Verifier[T] {
	t.Helper()
	verifier, err := NewVerifier[T](key, algorithm)
	require.NoError(t, err)
	return verifier
}
