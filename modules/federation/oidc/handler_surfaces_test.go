package oidc

// handler_surfaces_test covers the phase-4 completion endpoints:
// introspection, users/me client surfaces, secret management, and
// the allowed-groups replace.

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/storage"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// tinyPNG is a valid 1x1 PNG used by the logo lifecycle test.
var tinyPNG = []byte{
	0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x00, 0x00, 0x00, 0x0D,
	0x49, 0x48, 0x44, 0x52, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1F, 0x15, 0xC4, 0x89, 0x00, 0x00, 0x00,
	0x0A, 0x49, 0x44, 0x41, 0x54, 0x78, 0x9C, 0x63, 0x00, 0x01, 0x00, 0x00,
	0x05, 0x00, 0x01, 0x0D, 0x0A, 0x2D, 0xB4, 0x00, 0x00, 0x00, 0x00, 0x49,
	0x45, 0x4E, 0x44, 0xAE, 0x42, 0x60, 0x82,
}

// uploadLogo builds a multipart logo upload body.
func uploadLogo(t *testing.T, data []byte) (string, *strings.Reader) {
	t.Helper()
	var buf strings.Builder
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "logo.png")
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, w.Close())
	return w.FormDataContentType(), strings.NewReader(buf.String())
}

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

// TestClientMetaAndPreview covers GET /clients/{id}/meta and
// GET /clients/{id}/preview/{userId}: the trimmed metadata view and
// the three claim maps a real authorization would mint.
func TestClientMetaAndPreview(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})
	client := clientFixture(ctx, t, store, "meta-"+stamp())

	// Meta: trimmed view with snake_case keys.
	req := signInRequest(http.MethodGet, clientsAPIPrefix+"/"+client.ID.String()+"/meta")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var meta struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &meta))
	assert.Equal(t, client.ID.String(), meta.Data["id"])
	assert.Equal(t, "standard", meta.Data["client_type"])
	assert.Equal(t, false, meta.Data["has_logo"])

	// Invalid client id → 400; unknown → 404.
	req = signInRequest(http.MethodGet, clientsAPIPrefix+"/garbage/meta")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)

	req = signInRequest(http.MethodGet, clientsAPIPrefix+"/"+NewID().String()+"/meta")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)

	// Preview: id_token / access_token / user_info claim maps.
	target := clientsAPIPrefix + "/" + client.ID.String() + "/preview/" + createdUser.String() +
		"?scopes=" + url.QueryEscape("openid profile email groups")
	req = signInRequest(http.MethodGet, target)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var preview struct {
		Data struct {
			IDToken     map[string]any `json:"id_token"`
			AccessToken map[string]any `json:"access_token"`
			UserInfo    map[string]any `json:"user_info"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &preview))
	assert.Equal(t, "https://sso.test", preview.Data.IDToken["iss"])
	assert.Contains(t, preview.Data.IDToken["aud"], client.ID.String())
	assert.Equal(t, client.ID.String(), preview.Data.AccessToken["client_id"])
	assert.Equal(t, "openid profile email groups", preview.Data.AccessToken["scope"])
	assert.Contains(t, preview.Data.UserInfo["email"], "@example.com")

	// Unknown user → 404.
	req = signInRequest(http.MethodGet, clientsAPIPrefix+"/"+client.ID.String()+"/preview/user_"+stamp())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestClientLogoLifecycle covers the phase 9C logo surface: upload →
// bare-bytes read (public) → meta has_logo flips → delete → 404.
func TestClientLogoLifecycle(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	blobs, err := storage.New(config.StorageConfig{DataDir: t.TempDir()})
	require.NoError(t, err)
	service.images = blobs

	users := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, users, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})
	client := clientFixture(ctx, t, store, "logo-"+stamp())

	// No logo yet → 404 (public route, no cookie needed).
	req := httptest.NewRequest(http.MethodGet, "/oidc/clients/"+client.ID.String()+"/logo", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)

	// Upload (admin) → 204; image_type syncs for the meta view.
	contentType, body := uploadLogo(t, tinyPNG)
	req = httptest.NewRequest(http.MethodPost, clientsAPIPrefix+"/"+client.ID.String()+"/logo", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(&http.Cookie{Name: "tango_session", Value: "session-token-1"})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())

	updated, err := store.GetClient(ctx, client.ID)
	require.NoError(t, err)
	require.NotNil(t, updated.LogoPath)
	require.NotNil(t, updated.ImageType)
	assert.Equal(t, "png", *updated.ImageType)

	// Public read serves the bytes bare.
	req = httptest.NewRequest(http.MethodGet, "/oidc/clients/"+client.ID.String()+"/logo", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, tinyPNG, rec.Body.Bytes())

	// Meta view flips has_logo.
	req = signInRequest(http.MethodGet, clientsAPIPrefix+"/"+client.ID.String()+"/meta")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"has_logo":true`)

	// Delete (admin) → 204, read is 404 again, image_type cleared.
	req = signInRequest(http.MethodDelete, clientsAPIPrefix+"/"+client.ID.String()+"/logo")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)

	cleared, err := store.GetClient(ctx, client.ID)
	require.NoError(t, err)
	assert.Nil(t, cleared.LogoPath)
	assert.Nil(t, cleared.ImageType)

	req = httptest.NewRequest(http.MethodGet, "/oidc/clients/"+client.ID.String()+"/logo", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}
