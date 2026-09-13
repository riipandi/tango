package oidc

// handler_surfaces_test covers the phase-4 completion endpoints:
// introspection, users/me client surfaces, secret management, and
// the allowed-groups replace.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// boolPtr returns a pointer to b.
func boolPtr(b bool) *bool { return &b }

// TestIntrospection exercises RFC 7662: active for the owning
// client, inactive otherwise or for garbage.
func TestIntrospection(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()
	verifier := "test-verifier-very-long-enough-for-pkce"

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})

	client := clientFixture(ctx, t, store, "rp-"+stamp())
	other := clientFixture(ctx, t, store, "other-"+stamp())

	code := authorizeCode(t, router, client, verifier, "n-introspect")
	status, body := postForm(t, router, tokenAPIPath, exchangeCodeForm(client, code, verifier), client.ID.String(), "rp-secret")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	access, _ := body["access_token"].(string)
	require.NotEmpty(t, access)

	// Owning client → active.
	form := "token=" + access
	req := httptest.NewRequest(http.MethodPost, introspectAPIPath, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.ID.String(), "rp-secret")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"active":true`)

	// A different client sees the token as inactive.
	req = httptest.NewRequest(http.MethodPost, introspectAPIPath, strings.NewReader(form))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(other.ID.String(), "rp-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"active":false`)

	// Garbage token → inactive (still 200).
	req = httptest.NewRequest(http.MethodPost, introspectAPIPath, strings.NewReader("token=garbage"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.ID.String(), "rp-secret")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Contains(t, rec.Body.String(), `"active":false`)
}

// TestUsersMeClientSurfaces covers users/me/clients and the
// authorized-clients list + revoke with token cascade.
func TestUsersMeClientSurfaces(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()
	verifier := "test-verifier-very-long-enough-for-pkce"

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	auth := &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())}
	router := newRouter(t, service, auth)

	// Reachable: an unrestricted client. Unreachable: restricted
	// with an allowlist that excludes the user.
	open := clientFixture(ctx, t, store, "open-"+stamp())
	restricted := clientFixture(ctx, t, store, "restricted-"+stamp())
	if _, err := store.UpdateClient(ctx, restricted.ID, ClientUpdateParams{IsGroupRestricted: boolPtr(true)}); err != nil {
		t.Fatalf("restrict client: %v", err)
	}

	req := signInRequest(http.MethodGet, "/oidc/users/me/clients")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), open.ID.String())
	assert.NotContains(t, rec.Body.String(), restricted.ID.String())

	// Authorize + exchange → the consent record appears.
	code := authorizeCode(t, router, open, verifier, "")
	status, body := postForm(t, router, tokenAPIPath, exchangeCodeForm(open, code, verifier), open.ID.String(), "rp-secret")
	require.Equal(t, http.StatusOK, status)

	req = signInRequest(http.MethodGet, "/oidc/users/me/authorized-clients")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), open.ID.String())

	// Admin view over the same records.
	req = httptest.NewRequest(http.MethodGet, "/oidc/users/"+createdUser.String()+"/authorized-clients", nil)
	req.AddCookie(&http.Cookie{Name: "tango_session", Value: "session-token-1"})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	// Revoke → consent gone + active tokens dead.
	req = signInRequest(http.MethodDelete, "/oidc/users/me/authorized-clients/"+open.ID.String())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	req = signInRequest(http.MethodGet, "/oidc/users/me/authorized-clients")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.NotContains(t, rec.Body.String(), open.ID.String())

	refresh, _ := body["refresh_token"].(string)
	status, _ = postForm(t, router, tokenAPIPath, url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refresh},
	}, open.ID.String(), "rp-secret")
	assert.Equal(t, http.StatusUnauthorized, status, "revoked authorization kills the family")
}

// TestClientSecretsLifecycle covers multi-secret management:
// create (raw returned once) → authenticate with it → list → delete
// → authenticate fails.
func TestClientSecretsLifecycle(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})
	client := clientFixture(ctx, t, store, "rp-"+stamp())

	// Create a second secret with a supplied value.
	payload := `{"secret":"supplied-secret-123456"}`
	req := signInRequest(http.MethodPost, clientsAPIPrefix+"/"+client.ID.String()+"/secrets", payload)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &envelope))
	secretID, _ := envelope.Data["id"].(string)
	rawSecret, _ := envelope.Data["secret"].(string)
	require.NotEmpty(t, secretID)
	require.Equal(t, "supplied-secret-123456", rawSecret)

	// List shows metadata without values.
	req = signInRequest(http.MethodGet, clientsAPIPrefix+"/"+client.ID.String()+"/secrets")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.NotContains(t, rec.Body.String(), "supplied-secret")

	// The new secret authenticates token requests (the unknown
	// refresh token surfaces invalid_grant, not invalid_client).
	req = httptest.NewRequest(http.MethodPost, tokenAPIPath, strings.NewReader(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"none"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.ID.String(), "supplied-secret-123456")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_grant")

	// Delete → authentication with it fails.
	req = signInRequest(http.MethodDelete, clientsAPIPrefix+"/"+client.ID.String()+"/secrets/"+secretID)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	req = httptest.NewRequest(http.MethodPost, tokenAPIPath, strings.NewReader(url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"none"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(client.ID.String(), "supplied-secret-123456")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
	assert.Contains(t, rec.Body.String(), "invalid_client")
}

// TestUpdateAllowedGroups covers PUT /clients/{id}/allowed-user-groups.
func TestUpdateAllowedGroups(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})

	client := clientFixture(ctx, t, store, "rp-"+stamp())

	// Seed a group via its own store.
	groups := usergroup.NewPostgresStore(ds)
	group, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "grp_" + stamp(), DisplayName: "Group",
	})
	require.NoError(t, err)

	payload := `{"user_group_ids":["` + group.ID.String() + `"]}`
	req := signInRequest(http.MethodPut, clientsAPIPrefix+"/"+client.ID.String()+"/allowed-user-groups", payload)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), group.ID.UUID())
}
