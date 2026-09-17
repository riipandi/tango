package oidc

// device_test.go pins the RFC 8628 device grant and the RFC 9126
// pushed authorization request over real Postgres: the state machine
// (pending → approved/denied → consumed), poll discipline, one-time
// PAR consumption, and the discovery metadata that advertises both.

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"net/http/httptest"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/federation/discovery"
	"github.com/riipandi/tango/modules/identity/user"
)

// deviceAuthorize runs the device authorization leg and returns the
// decoded RFC 8628 §3.2 response.
func deviceAuthorize(t *testing.T, router chi.Router, client Client) (int, map[string]any) {
	t.Helper()
	return postForm(t, router, "/oidc/device/authorize", url.Values{
		"client_id": {client.ID.String()},
		"scope":     {"openid"},
	}, client.ID.String(), "rp-secret")
}

// deviceVerify approves or denies a user code with the signed-in
// browser session.
func deviceVerify(t *testing.T, router chi.Router, userCode, action string) int {
	t.Helper()

	values := url.Values{"code": {userCode}}
	if action != "" {
		values.Set("action", action)
	}
	req := signInRequest(http.MethodPost, "/oidc/device/verify", values.Encode())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec.Code
}

// devicePoll runs one token poll against the device grant.
func devicePoll(t *testing.T, router chi.Router, client Client, deviceCode string) (int, map[string]any) {
	t.Helper()
	return postForm(t, router, tokenAPIPath, url.Values{
		"grant_type":  {GrantDeviceCode},
		"device_code": {deviceCode},
	}, client.ID.String(), "rp-secret")
}

// authorizeDeviceE2E drives the authorization leg and returns the
// codes; the shape of the §3.2 payload is pinned here.
func authorizeDeviceE2E(t *testing.T, router chi.Router, client Client) (deviceCode, userCode string) {
	t.Helper()

	status, body := deviceAuthorize(t, router, client)
	require.Equal(t, http.StatusOK, status, "body: %v", body)

	require.NotEmpty(t, body["device_code"])
	require.NotEmpty(t, body["user_code"])
	assert.Equal(t, float64(DevicePollInterval), body["interval"])
	assert.Equal(t, float64(int(DeviceCodeTTL.Seconds())), body["expires_in"])
	assert.True(t, strings.HasSuffix(body["verification_uri"].(string), "/device"),
		"verification_uri points at the SPA page")
	assert.True(t, strings.Contains(body["verification_uri_complete"].(string), "code="),
		"verification_uri_complete carries the user code")
	return body["device_code"].(string), body["user_code"].(string)
}

// deviceStack builds the router with a signed-in browser session for
// a real user row (the approval binds user_id).
func deviceStack(t *testing.T) (*Service, chi.Router, Store, user.UserID) {
	t.Helper()

	service, store, ds := testStack(t)
	ctx := t.Context()

	userStore := user.NewPostgresStore(ds)
	createdUser := userFixture(ctx, t, userStore, stamp())
	auth := &fakeAuthenticator{validToken: "session-token-1", principal: principalFixture(createdUser.String())}
	router := newRouter(t, service, auth)
	return service, router, store, createdUser
}

func TestDeviceFlowIssuesTokensAfterApproval(t *testing.T) {
	_, router, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-rp-"+stamp())
	deviceCode, userCode := authorizeDeviceE2E(t, router, client)

	// Poll before approval: pending.
	status, body := devicePoll(t, router, client, deviceCode)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "authorization_pending", body["error"])

	// The user approves in the browser.
	require.Equal(t, http.StatusNoContent, deviceVerify(t, router, userCode, ""))

	// The poll now wins the tokens: access, ID, and refresh.
	status, body = devicePoll(t, router, client, deviceCode)
	require.Equal(t, http.StatusOK, status, "body: %v", body)
	require.NotEmpty(t, body["access_token"])
	require.NotEmpty(t, body["id_token"])
	require.NotEmpty(t, body["refresh_token"])
	assert.Equal(t, "Bearer", body["token_type"])

	// Replay: the authorization is consumed; a second poll is invalid.
	status, body = devicePoll(t, router, client, deviceCode)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "invalid_grant", body["error"])
}

func TestDeviceFlowDenialDeniesThePoll(t *testing.T) {
	_, router, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-deny-"+stamp())
	deviceCode, userCode := authorizeDeviceE2E(t, router, client)

	require.Equal(t, http.StatusNoContent, deviceVerify(t, router, userCode, "deny"))

	status, body := devicePoll(t, router, client, deviceCode)
	assert.Equal(t, http.StatusForbidden, status)
	assert.Equal(t, "access_denied", body["error"])
}

func TestDevicePollEscalatesFastPollers(t *testing.T) {
	_, router, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-fast-"+stamp())
	deviceCode, _ := authorizeDeviceE2E(t, router, client)

	_, body := devicePoll(t, router, client, deviceCode)
	assert.Equal(t, "authorization_pending", body["error"])

	// An immediate second poll violates the advertised interval.
	status, body := devicePoll(t, router, client, deviceCode)
	assert.Equal(t, http.StatusBadRequest, status)
	assert.Equal(t, "slow_down", body["error"])
}

func TestDeviceCodeIsSingleApproval(t *testing.T) {
	_, router, store, createdUser := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-single-"+stamp())
	deviceCode, userCode := authorizeDeviceE2E(t, router, client)

	// A second approval is a conflict: no rebinding to another user.
	require.Equal(t, http.StatusNoContent, deviceVerify(t, router, userCode, ""))
	assert.Equal(t, http.StatusConflict, deviceVerify(t, router, userCode, ""))

	status, body := devicePoll(t, router, client, deviceCode)
	require.Equal(t, http.StatusOK, status, "the bound user still wins the tokens: %v", body)

	// The consent record exists.
	has, err := store.HasAuthorizedClient(ctx, createdUser.String(), client.ID.String())
	require.NoError(t, err)
	assert.True(t, has)
}

func TestDeviceAuthorizeRequiresClientAuth(t *testing.T) {
	_, router, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-anon-"+stamp())

	// A confidential client without credentials: the protocol
	// answers bare 401.
	req := httptest.NewRequest(http.MethodPost, "/oidc/device/authorize",
		strings.NewReader(url.Values{"client_id": {client.ID.String()}, "scope": {"openid"}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)

	var body map[string]any
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotEmpty(t, body["error"])
}

func TestPARPushAndOneTimeAuthorizeResume(t *testing.T) {
	_, router, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "par-rp-"+stamp())

	values := url.Values{
		"client_id":             {client.ID.String()},
		"redirect_uri":          {"https://rp.example/callback"},
		"response_type":         {"code"},
		"scope":                 {"openid profile"},
		"state":                 {"st-par"},
		"nonce":                 {"n-par"},
		"code_challenge":        {pkceS256("test-verifier-very-long-enough-for-pkce")},
		"code_challenge_method": {"S256"},
	}
	push, err := http.NewRequest(http.MethodPost, "/oidc/par", strings.NewReader(values.Encode()))
	require.NoError(t, err)
	push.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	push.SetBasicAuth(client.ID.String(), "rp-secret")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, push)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var pushed struct {
		RequestURI string `json:"request_uri"`
		ExpiresIn  int    `json:"expires_in"`
	}
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &pushed))
	require.NotEmpty(t, pushed.RequestURI)
	assert.True(t, strings.HasPrefix(pushed.RequestURI, "urn:ietf:params:oauth:request_uri:"))
	assert.Equal(t, int(PARTTL.Seconds()), pushed.ExpiresIn)

	// The authorize resume redirects into the interaction flow with
	// the pushed parameters; the pushed row is one-time use.
	resume := signInRequest(http.MethodGet, "/authorize?"+url.Values{
		"request_uri": {pushed.RequestURI},
	}.Encode())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, resume)
	assert.Equal(t, http.StatusFound, rec.Code, "the resume redirects to the interaction")
	assert.NotEmpty(t, rec.Header().Get("Location"))

	replay := signInRequest(http.MethodGet, "/authorize?"+url.Values{
		"request_uri": {pushed.RequestURI},
	}.Encode())
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, replay)
	assert.Equal(t, http.StatusBadRequest, rec.Code, "a consumed request_uri is rejected")
}

func TestDiscoveryAdvertisesDeviceAndPAR(t *testing.T) {
	// The document is served by the discovery feature; build it over
	// the same issuer the oidc test stack uses.
	feature := discovery.New(newStaticKeyProvider(t), "https://sso.test")
	router := chi.NewRouter()
	feature.Routes(router)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/.well-known/openid-configuration", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var doc struct {
		DeviceAuthorizationEndpoint        string   `json:"device_authorization_endpoint"`
		PushedAuthorizationRequestEndpoint string   `json:"pushed_authorization_request_endpoint"`
		IntrospectionEndpoint              string   `json:"introspection_endpoint"`
		GrantTypesSupported                []string `json:"grant_types_supported"`
		ResponseModesSupported             []string `json:"response_modes_supported"`
		PromptValuesSupported              []string `json:"prompt_values_supported"`
		AuthorizationResponseIss           bool     `json:"authorization_response_iss_parameter_supported"`
	}
	require.NoError(t, jsonv2.Unmarshal(rec.Body.Bytes(), &doc))

	assert.Equal(t, "https://sso.test/api/oidc/device/authorize", doc.DeviceAuthorizationEndpoint)
	assert.Equal(t, "https://sso.test/api/oidc/par", doc.PushedAuthorizationRequestEndpoint)
	assert.Equal(t, "https://sso.test/api/oidc/introspect", doc.IntrospectionEndpoint)
	assert.Contains(t, doc.GrantTypesSupported, GrantDeviceCode)
	assert.NotEmpty(t, doc.ResponseModesSupported)
	assert.NotEmpty(t, doc.PromptValuesSupported)
	assert.True(t, doc.AuthorizationResponseIss)
}

func TestDeviceCodesExpireAndPrune(t *testing.T) {
	_, _, store, _ := deviceStack(t)
	ctx := t.Context()

	client := clientFixture(ctx, t, store, "device-expire-"+stamp())
	// The CHECK rejects inserting an already-past expiry; mint a
	// 1-second window and let it lapse.
	code := DeviceCode{
		DeviceCodeHash: sha256Hex("expired-device"),
		UserCodeHash:   sha256Hex("EEXPIRED"),
		Scope:          "openid",
		ClientID:       client.ID.String(),
		Status:         DeviceStatusPending,
		ExpiresAt:      time.Now().UTC().Add(time.Second),
	}
	require.NoError(t, store.InsertDeviceCode(ctx, code))
	time.Sleep(1100 * time.Millisecond)

	// The row still loads (expiry is enforced at the poll/consume
	// layer), and pruning sweeps it.
	loaded, err := store.GetDeviceCode(ctx, sha256Hex("expired-device"))
	require.NoError(t, err)
	assert.True(t, loaded.ExpiresAt.Before(time.Now().UTC()))

	removed, err := store.PruneDeviceCodes(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, int64(1), removed)

	_, err = store.GetDeviceCode(ctx, sha256Hex("expired-device"))
	assert.ErrorIs(t, err, ErrInvalidGrant)
}

// TestDeviceVerifyAndInfoRejectMissingCode pins the manual protocol
// parsing contract: a verify/info call without a user code is a 400
// before any store work.
func TestDeviceVerifyAndInfoRejectMissingCode(t *testing.T) {
	_, router, _, _ := deviceStack(t)

	// Verify without a code.
	req := signInRequest(http.MethodPost, "/oidc/device/verify", url.Values{}.Encode())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing code")

	// Info without a code.
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/oidc/device/info", nil))
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "missing code")
}
