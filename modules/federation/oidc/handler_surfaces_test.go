package oidc

// handler_surfaces_test covers introspection through the retained
// REST protocol surface.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/identity/user"
)

// uploadLogo builds a multipart logo upload body.

// boolPtr returns a pointer to b.

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

// TestClientSecretsLifecycle covers multi-secret management:
// create (raw returned once) → authenticate with it → list → delete
// → authenticate fails.

// TestUpdateAllowedGroups covers PUT /clients/{id}/allowed-user-groups.

// TestClientMetaAndPreview covers GET /clients/{id}/meta and
// GET /clients/{id}/preview/{userId}: the trimmed metadata view and
// the three claim maps a real authorization would mint.

// TestClientLogoLifecycle covers upload, public read, metadata, and delete.
