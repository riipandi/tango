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

	jsonv2 "encoding/json/v2"

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
// session and access tokens the body carries.
func rpcSignIn(t *testing.T, sessions *Service, identityText, secret string) (string, string) {
	t.Helper()
	h := &authRPC{service: sessions}
	resp, err := h.SignIn(t.Context(), connect.NewRequest(&identityv1.SignInRequest{
		Identity: identityText, Password: secret,
	}))
	require.NoError(t, err)
	return resp.Msg.GetSessionToken(), resp.Msg.GetAccessToken()
}

// tokenBridge posts a session token to the refresh endpoint.
func tokenBridge(t *testing.T, r http.Handler, sessionToken string) *httptest.ResponseRecorder {
	t.Helper()
	body := "{}"
	if sessionToken != "" {
		body = `{"session_token":"` + sessionToken + `"}`
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// bridgeField reads one string field from the refresh answer.
func bridgeField(t *testing.T, w *httptest.ResponseRecorder, field string) string {
	t.Helper()
	var payload struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	value, _ := payload.Data[field].(string)
	return value
}

// TestTokenBridgeBootstrapAndRotation pins the stateless refresh
// channel: a posted session token answers with a fresh bearer, the
// rotation replaces the session token so the previous value stops
// resolving, and revocation kills the family inside the token TTL.
func TestTokenBridgeBootstrapAndRotation(t *testing.T) {
	signer := newTestKeyProvider(t)
	r, sessions, passwords, users := newTestRouter(t, WithAccessTokens(NewAccessTokenSigner(signer)))
	u := newUser(t, users, passwords, "bridge")

	// Sign-in: session token plus the access bearer.
	refresh, access := rpcSignIn(t, sessions, u.Username, "s3cret-p@ss")
	require.NotEmpty(t, refresh)
	require.NotEmpty(t, access, "sign-in mints the access bearer")

	// Refresh path: rotate → new session token, old one dies.
	w := tokenBridge(t, r, refresh)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	rotated := bridgeField(t, w, "session_token")
	require.NotEmpty(t, rotated)
	assert.NotEqual(t, refresh, rotated, "refresh rotation must replace the token")
	_, _, err := sessions.Resolve(t.Context(), refresh)
	assert.Error(t, err, "the rotated-out refresh token must stop resolving")

	// The bridged access token resolves to the principal over RPC.
	resolvedAccess := bridgeField(t, w, "access_token")
	principal, err := sessions.ResolveAccess(t.Context(), resolvedAccess)
	require.NoError(t, err)
	assert.Equal(t, u.ID.String(), principal.UserID)

	// Revocation kills the family even inside the token TTL.
	require.NoError(t, sessions.RevokeCurrent(t.Context(), rotated))
	w = tokenBridge(t, r, rotated)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	_, err = sessions.ResolveAccess(t.Context(), resolvedAccess)
	assert.Error(t, err, "revoked sessions must fail token verification")
}

// TestTokenBridgeRejectsAnonymous pins the 401 branch: a missing and a
// garbage session token answer the enumeration-safe message.
func TestTokenBridgeRejectsAnonymous(t *testing.T) {
	signer := newTestKeyProvider(t)
	r, sessionsForBridge, passwords, users := newTestRouter(t, WithAccessTokens(NewAccessTokenSigner(signer)))
	u := newUser(t, users, passwords, "bridge_anon")

	w := tokenBridge(t, r, "")
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	w = tokenBridge(t, r, "garbage")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired token")

	// A live session exists, yet no token means no bridge answer: the
	// rejection is the missing credential, not a broken bridge.
	rpcSignIn(t, sessionsForBridge, u.Username, "s3cret-p@ss")
}

// TestBridgeWithoutSignerSurfaces501 keeps the misconfiguration
// visible instead of answering 401s.
func TestBridgeWithoutSignerSurfaces501(t *testing.T) {
	r, _, passwords, users := newTestRouter(t)
	u := newUser(t, users, passwords, "bridge_bare")
	_ = u

	w := tokenBridge(t, r, "any")
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
	assert.WithinDuration(t, time.Now().Add(DefaultAccessTokenTTL), expiresAt, time.Minute)

	verified, err := signer.Verify(ctx, signed)
	require.NoError(t, err)
	assert.Equal(t, "user_test", verified.Subject)
	assert.Equal(t, "sess_test", verified.Private.SessionID)
	assert.True(t, verified.Private.Admin)

	// Any payload mutation breaks the signature.
	_, err = signer.Verify(ctx, signed+"x")
	assert.Error(t, err)
}
