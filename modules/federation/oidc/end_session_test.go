package oidc

// end_session_test.go drives the RP-initiated logout contract: the
// hint must verify against the published keys, the grant dies with
// it, and the redirect follows the registered logout callbacks.

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/identity/user"
)

func TestEndSessionWithoutHintFallsBack(t *testing.T) {
	service, _, _ := testStack(t)

	w := httptest.NewRecorder()
	service.handleEndSession(w, httptest.NewRequest(http.MethodPost, "/api/oidc/end-session", nil))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://sso.test/logout", w.Header().Get("Location"))
}

func TestEndSessionRevokesFamilyAndRedirects(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-"+stamp())
	_, err := store.UpdateClient(ctx, client.ID, ClientUpdateParams{
		LogoutCallbackURLs: []string{"https://rp.example/logged-out"},
	})
	require.NoError(t, err)

	member := userFixture(ctx, t, user.NewPostgresStore(ds), stamp())
	require.NoError(t, store.UpsertAuthorizedClient(ctx, member.String(), client.ID.String(), []string{"openid"}))

	response, err := service.mintTokens(ctx, client, member.String(), "openid", "", "", seedFamily(NewID().String(), "sid-1", "password", time.Now().UTC()))
	require.NoError(t, err)

	// Before logout the access token family is active.
	jti := jwtJTI(t, response.AccessToken)
	assert.True(t, familyActive(t, store, jti))

	// End the session with the ID-token hint; the registered
	// callback wins and carries the state.
	w := httptest.NewRecorder()
	service.handleEndSession(w, formRequest(t, "/api/oidc/end-session", map[string]string{
		"id_token_hint":            response.IDToken,
		"client_id":                client.ID.String(),
		"post_logout_redirect_uri": "https://rp.example/logged-out",
		"state":                    "st-1",
	}))
	require.Equal(t, http.StatusFound, w.Code, w.Body.String())
	assert.Equal(t, "https://rp.example/logged-out?state=st-1", w.Header().Get("Location"))

	assert.Empty(t, w.Result().Cookies(), "end-session no longer clears a cookie")

	// The whole token family is inactive afterwards.
	assert.False(t, familyActive(t, store, jti))

	// A replayed hint is idempotent: the family is already inactive
	// and the registered callback still answers.
	w = httptest.NewRecorder()
	service.handleEndSession(w, formRequest(t, "/api/oidc/end-session", map[string]string{
		"id_token_hint": response.IDToken,
	}))
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://rp.example/logged-out", w.Header().Get("Location"))
}

func TestEndSessionRejectsUnregisteredCallback(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-"+stamp())
	_, err := store.UpdateClient(ctx, client.ID, ClientUpdateParams{
		LogoutCallbackURLs: []string{"https://rp.example/logged-out"},
	})
	require.NoError(t, err)

	member := userFixture(ctx, t, user.NewPostgresStore(ds), stamp())
	require.NoError(t, store.UpsertAuthorizedClient(ctx, member.String(), client.ID.String(), []string{"openid"}))

	response, err := service.mintTokens(ctx, client, member.String(), "openid", "", "", seedFamily(NewID().String(), "sid-1", "password", time.Now().UTC()))
	require.NoError(t, err)

	// An unregistered post-logout URI must not be followed.
	w := httptest.NewRecorder()
	service.handleEndSession(w, formRequest(t, "/api/oidc/end-session", map[string]string{
		"id_token_hint":            response.IDToken,
		"post_logout_redirect_uri": "https://evil.example/logged-out",
	}))
	require.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://sso.test/logout", w.Header().Get("Location"))

	// The contract failed before revocation: the family stays active.
	assert.True(t, familyActive(t, store, jwtJTI(t, response.AccessToken)))
}

// formRequest builds a form-encoded POST with the given fields.
func formRequest(t *testing.T, path string, fields map[string]string) *http.Request {
	t.Helper()
	form := strings.NewReader(encodeForm(fields))
	req := httptest.NewRequest(http.MethodPost, path, form)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// familyActive reports whether the access session row with the given
// jti is still active.
func familyActive(t *testing.T, store Store, jti string) bool {
	t.Helper()
	row, err := store.GetSession(t.Context(), KindAccessToken, jti)
	require.NoError(t, err)
	return row.Active
}

// jwtJTI decodes an unsigned JWT payload and returns its jti.
func jwtJTI(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	require.Len(t, parts, 3, "an access token is a three-part JWT")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	var claims struct {
		JTI string `json:"jti"`
	}
	require.NoError(t, jsonv2.Unmarshal(payload, &claims))
	return claims.JTI
}

func encodeForm(fields map[string]string) string {
	parts := make([]string, 0, len(fields))
	for k, v := range fields {
		parts = append(parts, urlEncode(k)+"="+urlEncode(v))
	}
	return strings.Join(parts, "&")
}

func urlEncode(s string) string {
	replacer := strings.NewReplacer("%", "%25", "&", "%26", "+", "%2B", "=", "%3D")
	return replacer.Replace(s)
}
