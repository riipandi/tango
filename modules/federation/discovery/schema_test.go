package discovery

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/riipandi/tango/pkg/jwtutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider serves one EC key as both signing and verification
// material.
type stubProvider struct{ key jwk.Key }

func newStubProvider(t *testing.T) *stubProvider {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	key, err := jwk.Import(priv)
	require.NoError(t, err)
	require.NoError(t, key.Set(jwk.KeyIDKey, "test-kid"))
	require.NoError(t, key.Set(jwk.KeyUsageKey, jwk.ForSignature))
	require.NoError(t, key.Set(jwk.AlgorithmKey, jwa.ES256()))
	return &stubProvider{key: key}
}

func (s *stubProvider) SignKey(context.Context) (jwk.Key, error) { return s.key, nil }

func (s *stubProvider) VerifyKeySet(context.Context) (jwk.Set, error) {
	pub, err := jwk.PublicKeyOf(s.key)
	if err != nil {
		return nil, err
	}
	set := jwk.NewSet()
	if err := set.AddKey(pub); err != nil {
		return nil, err
	}
	return set, nil
}

var _ jwtutils.KeyProvider = (*stubProvider)(nil)

func newRouter(t *testing.T) http.Handler {
	t.Helper()
	feature := New(newStubProvider(t), "https://sso.example.com/")
	r := chi.NewRouter()
	feature.Routes(r)
	return r
}

func TestJWKSHandler(t *testing.T) {
	handler := newRouter(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, JWKSPath, nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, jwksCacheControl, rec.Header().Get("Cache-Control"))

	var body struct {
		Keys []map[string]any `json:"keys"`
	}
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &body))
	require.Len(t, body.Keys, 1)
	assert.Equal(t, "test-kid", body.Keys[0]["kid"])
	assert.Equal(t, "EC", body.Keys[0]["kty"])
	assert.Equal(t, "sig", body.Keys[0]["use"])
	assert.Nil(t, body.Keys[0]["d"], "private material must never serialize")
}

func TestOpenIDConfigurationHandler(t *testing.T) {
	handler := newRouter(t)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, ConfigPath, nil))

	require.Equal(t, http.StatusOK, rec.Code)

	var doc discoveryDocument
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &doc))
	assert.Equal(t, "https://sso.example.com", doc.Issuer)
	assert.Equal(t, "https://sso.example.com"+JWKSURI, doc.JWKSURI)
	assert.Equal(t, "https://sso.example.com"+TokenEndpoint, doc.TokenEndpoint)
	assert.Equal(t, "https://sso.example.com"+AuthorizeEndpoint, doc.AuthorizationEndpoint)
	assert.Contains(t, doc.IDTokenSigningAlgValuesSupported, "RS256")
	assert.Equal(t, []string{"code"}, doc.ResponseTypesSupported)
	assert.True(t, doc.RequestParameterSupported)
	assert.Equal(t, "", doc.ServiceDocumentation)
}
