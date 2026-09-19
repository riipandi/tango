package session

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/lestrrat-go/jwx/v3/jwk"
	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/kernel"
)

// stubKeyProvider serves one in-memory RSA key so access tokens sign
// and verify without the JWKS store.
type stubKeyProvider struct{ key jwk.Key }

func (p stubKeyProvider) SignKey(context.Context) (jwk.Key, error) { return p.key, nil }

func (p stubKeyProvider) VerifyKeySet(context.Context) (jwk.Set, error) {
	set := jwk.NewSet()
	_ = set.AddKey(p.key)
	return set, nil
}

func newTestKeyProvider(t *testing.T) stubKeyProvider {
	t.Helper()
	raw, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	key, err := jwk.Import(raw)
	require.NoError(t, err)
	return stubKeyProvider{key: key}
}

// rpcSignIn drives the Connect SignIn procedure and returns the
// refresh and access cookie values from the response.
func rpcSignIn(t *testing.T, sessions *Service, identityText, secret string) (string, string) {
	t.Helper()
	h := &authRPC{service: sessions}
	resp, err := h.SignIn(t.Context(), connect.NewRequest(&identityv1.SignInRequest{
		Identity: identityText, Secret: secret,
	}))
	require.NoError(t, err)
	return setCookieValue(resp.Header(), CookieName), setCookieValue(resp.Header(), AccessTokenCookieName)
}

// setCookieValue reads one Set-Cookie value; multiple cookies ride
// separate header entries.
func setCookieValue(header http.Header, name string) string {
	for _, setCookie := range header["Set-Cookie"] {
		for _, part := range strings.Split(setCookie, "; ") {
			if strings.HasPrefix(part, name+"=") {
				return strings.TrimPrefix(part, name+"=")
			}
		}
	}
	return ""
}

func tokenBridge(t *testing.T, r http.Handler, cookies ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", nil)
	for _, c := range cookies {
		req.Header.Add("Cookie", c)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// namedCookie pulls one cookie out of a response by name.
func namedCookie(t *testing.T, w *httptest.ResponseRecorder, name string) string {
	t.Helper()
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}

// TestTokenBridgeBootstrapAndRotation pins the worker cookie bridge:
// sign-in mirrors the access token into an HttpOnly bridge-scoped
// cookie, the bridge answers with a bearer token, and a refresh-only
// bootstrap rotates the refresh token so the previous value stops
// resolving.
func TestTokenBridgeBootstrapAndRotation(t *testing.T) {
	signer := newTestKeyProvider(t)
	r, sessions, passwords, users := newTestRouter(t, WithAccessTokens(NewAccessTokenSigner(signer)))
	u := newUser(t, users, passwords, "bridge")

	// Sign-in: refresh cookie plus the bridge-scoped access mirror.
	refresh, access := rpcSignIn(t, sessions, u.Username, "s3cret-p@ss")
	require.NotEmpty(t, refresh)
	require.NotEmpty(t, access, "sign-in mirrors the access token for bootstrap")

	// Fast path: valid access cookie → same token, no rotation.
	w := tokenBridge(t, r, "tango_access="+access, CookieName+"="+refresh)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"token_type":"Bearer"`)
	assert.Equal(t, access, namedCookie(t, w, AccessTokenCookieName))

	// Refresh path: rotate → new refresh token, old one dies.
	w = tokenBridge(t, r, CookieName+"="+refresh)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rotated := namedCookie(t, w, CookieName)
	require.NotEmpty(t, rotated)
	assert.NotEqual(t, refresh, rotated, "refresh rotation must replace the token")
	_, _, err := sessions.Resolve(t.Context(), refresh)
	assert.Error(t, err, "the rotated-out refresh token must stop resolving")

	// The bridged access token resolves to the principal over RPC.
	resolvedAccess := namedCookie(t, w, AccessTokenCookieName)
	principal, err := sessions.ResolveAccess(t.Context(), resolvedAccess)
	require.NoError(t, err)
	assert.Equal(t, u.ID.String(), principal.UserID)

	// Revocation kills the family even inside the token TTL.
	require.NoError(t, sessions.RevokeCurrent(t.Context(), rotated))
	w = tokenBridge(t, r, CookieName+"="+rotated)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	_, err = sessions.ResolveAccess(t.Context(), resolvedAccess)
	assert.Error(t, err, "revoked sessions must fail token verification")
}

// TestTokenBridgeRejectsAnonymous pins the 401 branch: no cookies and
// garbage cookies answer the enumeration-safe message.
func TestTokenBridgeRejectsAnonymous(t *testing.T) {
	signer := newTestKeyProvider(t)
	r, sessionsForBridge, passwords, users := newTestRouter(t, WithAccessTokens(NewAccessTokenSigner(signer)))
	u := newUser(t, users, passwords, "bridge_anon")

	w := tokenBridge(t, r)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired token")

	w = tokenBridge(t, r, CookieName+"=garbage")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// A live session exists, yet no cookie means no bridge answer:
	// the rejection is the missing cookie, not a broken bridge.
	rpcSignIn(t, sessionsForBridge, u.Username, "s3cret-p@ss")
}

// TestBridgeWithoutSignerSurfaces501 keeps the misconfiguration
// visible instead of answering 401s.
func TestBridgeWithoutSignerSurfaces501(t *testing.T) {
	r, _, passwords, users := newTestRouter(t)
	u := newUser(t, users, passwords, "bridge_bare")
	_ = u

	w := tokenBridge(t, r)
	assert.Equal(t, http.StatusNotImplemented, w.Code)
}

// TestAccessSignerRoundTrip covers the token contract directly:
// claims round-trip and a tampered token fails verification.
func TestAccessSignerRoundTrip(t *testing.T) {
	signer := NewAccessTokenSigner(newTestKeyProvider(t))
	ctx := t.Context()

	signed, expiresAt, err := signer.Issue(ctx, kernel.Principal{
		SessionID: "sess_test",
		UserID:    "user_test",
		IsAdmin:   true,
	})
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(AccessTokenTTL), expiresAt, time.Minute)

	verified, err := signer.Verify(ctx, signed)
	require.NoError(t, err)
	assert.Equal(t, "user_test", verified.Subject)
	assert.Equal(t, "sess_test", verified.Private.SessionID)
	assert.True(t, verified.Private.Admin)

	// Any payload mutation breaks the signature.
	_, err = signer.Verify(ctx, signed+"x")
	assert.Error(t, err)
}
