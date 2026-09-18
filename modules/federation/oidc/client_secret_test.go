package oidc

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// clientAuthStatus sends a token request authenticated with the
// client credentials and returns the HTTP status.
func clientAuthStatus(t *testing.T, service *Service, clientID, secret string) int {
	t.Helper()
	router := newRouter(t, service, &fakeAuthenticator{validToken: "nope", principal: principalFixture("")})

	req := httptest.NewRequest(http.MethodPost, tokenAPIPath, strings.NewReader(url.Values{
		"grant_type": {"refresh_token"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

func TestClientSecretLifecycle(t *testing.T) {
	service, store, _ := testStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-lifecycle-"+stamp())
	require.Len(t, client.Secrets, 1, "create issues one active credentials entry")

	// The create-time secret authenticates.
	assert.Equal(t, http.StatusBadRequest, clientAuthStatus(t, service, client.ID.String(), "rp-secret"),
		"invalid_grant, not invalid_client — auth passed")

	// A second secret coexists: both authenticate (multi-secret).
	second := ClientSecret{ID: "second", CreatedAt: time.Now().UTC(), IsActive: true}
	require.NoError(t, store.AddClientSecret(ctx, client.ID, second, sha256Hex("second-secret")))
	assert.Equal(t, http.StatusBadRequest, clientAuthStatus(t, service, client.ID.String(), "second-secret"))
	assert.Equal(t, http.StatusBadRequest, clientAuthStatus(t, service, client.ID.String(), "rp-secret"))

	// An expired entry cannot authenticate; siblings still can.
	expired := ClientSecret{ID: "expired", CreatedAt: time.Now().UTC(), IsActive: true,
		ExpiresAt: ptrTime(time.Now().UTC().Add(-time.Minute))}
	require.NoError(t, store.AddClientSecret(ctx, client.ID, expired, sha256Hex("expired-secret")))
	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "expired-secret"))
	assert.Equal(t, http.StatusBadRequest, clientAuthStatus(t, service, client.ID.String(), "rp-secret"))

	// An inactive entry cannot authenticate.
	inactive := ClientSecret{ID: "inactive", CreatedAt: time.Now().UTC(), IsActive: false}
	require.NoError(t, store.AddClientSecret(ctx, client.ID, inactive, sha256Hex("inactive-secret")))
	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "inactive-secret"))

	// Rotation replaces the whole list: old raw values stop working.
	updated, err := store.UpdateClient(ctx, client.ID, ClientUpdateParams{SecretHash: ptrString(sha256Hex("rotated-secret"))})
	require.NoError(t, err)
	require.Len(t, updated.Secrets, 1)
	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "rp-secret"))
	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "second-secret"))
	assert.Equal(t, http.StatusBadRequest, clientAuthStatus(t, service, client.ID.String(), "rotated-secret"))

	// Deleting the last entry leaves no usable secret.
	require.NoError(t, store.DeleteClientSecret(ctx, client.ID, updated.Secrets[0].ID))
	after, err := store.GetClient(ctx, client.ID)
	require.NoError(t, err)
	assert.Empty(t, after.Secrets)
	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "rotated-secret"))
}

func TestClientSecretNoLegacyFallback(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-nolegacy-"+stamp())

	// A row without credentials entries must not authenticate: there
	// is no fallback reader, even if some column value existed.
	_, err := ds.Exec(ctx,
		`UPDATE public.oidc_clients SET credentials = NULL WHERE id = $1`,
		client.ID.String())
	require.NoError(t, err)

	assert.Equal(t, http.StatusUnauthorized, clientAuthStatus(t, service, client.ID.String(), "rp-secret"))
}

func ptrString(v string) *string { return &v }

func ptrTime(v time.Time) *time.Time { return &v }
