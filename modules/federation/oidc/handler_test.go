package oidc

// handler_test drives the E2E flows over the chi router: client
// CRUD (admin guard), authorize → code → token → userinfo, and
// refresh-token rotation with reuse revocation.

import (
	"context"
	jsonv2 "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/identity/user"
)

// fakeAuthenticator resolves a fixed principal for the signed-in
// cookie value; other tokens fail.
type fakeAuthenticator struct {
	validToken string
	principal  middleware.Principal
}

func (f *fakeAuthenticator) ResolveSession(_ context.Context, token string) (middleware.Principal, error) {
	if token == f.validToken {
		return f.principal, nil
	}
	return middleware.Principal{}, ErrInvalidGrant
}

// newRouter builds the feature over the test stack, with the admin
// guard mounted for client management.
func newRouter(t *testing.T, service *Service, auth *fakeAuthenticator) chi.Router {
	t.Helper()
	feature := New(service)
	adminGuard := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(auth, "tango_session")(middleware.RequireAdmin(next))
	}
	selfGuard := middleware.RequireAuth(auth, "tango_session")

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if cookie, err := req.Cookie("tango_session"); err == nil && cookie.Value != "" {
				if principal, err := auth.ResolveSession(req.Context(), cookie.Value); err == nil {
					next.ServeHTTP(w, req.WithContext(middleware.WithPrincipal(req.Context(), principal)))
					return
				}
			}
			next.ServeHTTP(w, req)
		})
	})
	feature.Routes(r)
	feature.APIRoutes(r, federation.RouteGroups{Admin: adminGuard, Self: selfGuard})
	return r
}

// signInRequest returns a request with the signed-in session cookie.
func signInRequest(method, target string, body ...string) *http.Request {
	var reader *strings.Reader
	if len(body) > 0 {
		reader = strings.NewReader(body[0])
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, target, reader)
	req.AddCookie(&http.Cookie{Name: "tango_session", Value: "session-token-1"})
	return req
}

// principalFixture is the signed-in test user.
func principalFixture(userID string) middleware.Principal {
	return middleware.Principal{
		SessionID: "auth-session-1",
		UserID:    userID,
		Username:  "abbey",
		Email:     "abbey@example.com",
		IsAdmin:   true,
	}
}

// userFixture inserts a user row (the claims source) with a fixed,
// valid UUID derived from the fixture principal.
func userFixture(ctx context.Context, t *testing.T, store user.Store, suffix string) user.UserID {
	t.Helper()
	created, err := store.Create(ctx, user.CreateParams{
		Username: "abbey_" + suffix,
		Email:    "abbey_" + suffix + "@example.com",
		IsAdmin:  true,
	})
	require.NoError(t, err)
	return created.ID
}

// postForm posts a form body and decodes the JSON response into a
// map (enveloped or bare — token errors are bare).
func postForm(t *testing.T, router chi.Router, target string, values url.Values, basicUser, basicPass string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	var body map[string]any
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &body), "body: %s", rec.Body.String())
	return rec.Code, body
}

// authorizeCode runs the signed-in /authorize leg and returns the
// issued code.
func authorizeCode(t *testing.T, router chi.Router, client Client, verifier, nonce string) string {
	t.Helper()
	req := signInRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"https://rp.example/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid email profile groups"},
		"state":                 {"st-1"},
		"nonce":                 {nonce},
		"code_challenge":        {pkceS256(verifier)},
		"code_challenge_method": {"S256"},
	}.Encode())

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusFound, rec.Code, "status=%d body=%s location=%q", rec.Code, rec.Body.String(), rec.Header().Get("Location"))

	callback, err := url.Parse(rec.Header().Get("Location"))
	require.NoError(t, err)
	code := callback.Query().Get("code")
	require.NotEmpty(t, code, "signed-in /authorize must issue a code")
	assert.Equal(t, "st-1", callback.Query().Get("state"))
	return code
}

// exchangeCodeForm builds the token exchange form for a code.
func exchangeCodeForm(client Client, code, verifier string) url.Values {
	return url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {"https://rp.example/callback"},
		"code_verifier": {verifier},
	}
}

func TestAuthorizeRedirectsAnonymousToInteraction(t *testing.T) {
	service, store, _ := testStack(t)
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture("")})
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-"+stamp())

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"https://rp.example/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid email profile"},
		"state":                 {"st-1"},
		"nonce":                 {"n-1"},
		"code_challenge":        {pkceS256("test-verifier-very-long-enough-for-pkce")},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
	assert.Contains(t, rec.Header().Get("Location"), InteractionPath+"/")
}

func TestAuthorizeRejectsPlainPKCE(t *testing.T) {
	service, store, _ := testStack(t)
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture("")})
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-"+stamp())

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"https://rp.example/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"code_challenge":        {"plaintext-challenge"},
		"code_challenge_method": {"plain"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Request rejected before the redirect_uri is trusted — the
	// error stays on the page (400), never a cross-origin redirect.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Empty(t, rec.Header().Get("Location"))
}

func TestAuthorizeRejectsUnregisteredCallback(t *testing.T) {
	service, store, _ := testStack(t)
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture("")})
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "rp-"+stamp())

	req := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"https://evil.example/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid"},
		"code_challenge":        {pkceS256("test-verifier-very-long-enough-for-pkce")},
		"code_challenge_method": {"S256"},
	}.Encode(), nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code, "unregistered callbacks never redirect")
}

func TestEndToEndAuthorizeTokenUserinfo(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()
	verifier := "test-verifier-very-long-enough-for-pkce"
	suffix := stamp()

	// The claims reader resolves users from the identity store.
	userStore := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, userStore, suffix)
	auth := &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())}
	router := newRouter(t, service, auth)

	client := clientFixture(ctx, t, store, "rp-"+stamp())

	// 1. authorize → code.
	code := authorizeCode(t, router, client, verifier, "n-e2e")

	// 2. token exchange (Basic auth).
	status, body := postForm(t, router, tokenAPIPath, exchangeCodeForm(client, code, verifier), client.ID.String(), "rp-secret")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	tokens := body

	access, _ := tokens["access_token"].(string)
	idToken, _ := tokens["id_token"].(string)
	refresh, _ := tokens["refresh_token"].(string)
	require.NotEmpty(t, access)
	require.NotEmpty(t, idToken)
	require.NotEmpty(t, refresh)
	assert.Equal(t, "Bearer", tokens["token_type"])
	assert.Equal(t, "openid email profile groups", tokens["scope"])

	// 3. userinfo with the bearer token.
	req := httptest.NewRequest(http.MethodGet, userinfoAPIPath, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var profile map[string]any
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &profile))
	assert.Equal(t, createdUser.UUID(), profile["sub"])
	assert.Equal(t, "abbey_"+suffix, profile["preferred_username"])

	// 4. userinfo without a token → 401.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, userinfoAPIPath, nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	// 5. The same code never issues tokens twice.
	status, body = postForm(t, router, tokenAPIPath, exchangeCodeForm(client, code, verifier), client.ID.String(), "rp-secret")
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "invalid_grant", body["error"])
}

func TestRefreshRotationAndReuseRevocation(t *testing.T) {
	service, store, ds := testStack(t)
	ctx := t.Context()
	verifier := "test-verifier-very-long-enough-for-pkce"

	userStore := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, userStore, stamp())
	auth := &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())}
	router := newRouter(t, service, auth)
	client := clientFixture(ctx, t, store, "rp-"+stamp())

	// Original exchange.
	code := authorizeCode(t, router, client, verifier, "")
	status, body := postForm(t, router, tokenAPIPath, exchangeCodeForm(client, code, verifier), client.ID.String(), "rp-secret")
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	firstRefresh, _ := body["refresh_token"].(string)
	require.NotEmpty(t, firstRefresh)

	// First rotation succeeds.
	rotate := func(refresh string) (int, map[string]any) {
		return postForm(t, router, tokenAPIPath, url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {refresh},
		}, client.ID.String(), "rp-secret")
	}
	status, body = rotate(firstRefresh)
	require.Equal(t, http.StatusOK, status, "body: %s", body)
	secondRefresh, _ := body["refresh_token"].(string)
	require.NotEmpty(t, secondRefresh)
	assert.NotEqual(t, firstRefresh, secondRefresh)

	// Replaying the rotated token kills the whole family.
	status, body = rotate(firstRefresh)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "invalid_grant", body["error"])

	// The live token from the same family is now revoked too.
	status, body = rotate(secondRefresh)
	assert.Equal(t, http.StatusUnauthorized, status)
	assert.Equal(t, "invalid_grant", body["error"])
}

func TestClientCRUDRequiresAdmin(t *testing.T) {
	service, _, _ := testStack(t)
	// Anonymous principal: admin guard rejects before handlers run.
	router := newRouter(t, service, &fakeAuthenticator{validToken: "nope", principal: principalFixture("")})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, clientsAPIPrefix+"/", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestClientCRUDLifecycle(t *testing.T) {
	service, _, ds := testStack(t)
	ctx := t.Context()

	userStore := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, userStore, stamp())
	router := newRouter(t, service, &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())})
	payload := `{"name":"RP","callback_urls":["https://rp.example/callback"],"is_public":false}`
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, signInRequest(http.MethodPost, clientsAPIPrefix+"/", payload))
	require.Equal(t, http.StatusCreated, rec.Code, "body: %s", rec.Body.String())

	var envelope struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &envelope))
	clientID, _ := envelope.Data["id"].(string)
	secret, _ := envelope.Data["client_secret"].(string)
	require.NotEmpty(t, clientID)
	require.NotEmpty(t, secret, "raw secret is returned exactly once")

	// GET never returns the raw secret.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, signInRequest(http.MethodGet, clientsAPIPrefix+"/"+clientID, ""))
	require.Equal(t, http.StatusOK, rec.Code)
	var fetched struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &fetched))
	assert.NotContains(t, fetched.Data, "client_secret")
	assert.Equal(t, true, fetched.Data["has_secret"])

	// The presented secret authenticates token requests.
	req := httptest.NewRequest(http.MethodPost, tokenAPIPath, strings.NewReader(url.Values{
		"grant_type": {"refresh_token"},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "invalid_grant, not invalid_client — auth passed")
}
