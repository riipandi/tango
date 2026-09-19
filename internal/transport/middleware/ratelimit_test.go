package middleware

// ratelimit_test.go pins the per-endpoint policy matrix: sensitive
// endpoints carry isolated budgets, everything else passes through
// without touching the store.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLimiter records the budget it was called with.
type fakeLimiter struct {
	key    string
	max    int
	window int
	calls  int
}

func (f *fakeLimiter) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	f.calls++
	f.key = args[0].(string)
	f.max = args[1].(int)
	f.window = args[2].(int)
	return fakeRow{payload: []byte(`{"limit":1,"remaining":1,"reset":0}`)}
}

// failingLimiter surfaces a store error; a plain (non-42901) error must
// fail open.
type failingLimiter struct{ err error }

func (f failingLimiter) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	return failingRow(f)
}

type failingRow struct{ err error }

func (r failingRow) Scan(dest ...any) error { return r.err }

// fakeRow satisfies pgx.Row with a fixed JSON result.
type fakeRow struct{ payload []byte }

func (r fakeRow) Scan(dest ...any) error {
	*(dest[0].(*[]byte)) = r.payload
	return nil
}

func TestPolicyForMapsSensitiveEndpoints(t *testing.T) {
	cases := []struct {
		method string
		path   string
		policy string
		max    int
	}{
		{http.MethodPost, "/rpc/tango.identity.v1.AuthService/SignIn", "sign-in", 20},
		{http.MethodPost, "/api/auth/forgot-password", "forgot-password", 2},
		{http.MethodPost, "/api/auth/reset-password", "reset-password", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.MfaService/EnrollTotp", "totp-enroll", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.MfaService/ConfirmTotp", "totp-confirm", 10},
		{http.MethodPost, "/rpc/tango.identity.v1.MfaService/VerifyPending", "totp-verify", 10},
		{http.MethodPost, "/rpc/tango.identity.v1.MfaService/RotateRecoveryCodes", "totp-recovery-codes", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.MfaService/DisableTotp", "totp-disable", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.SignupService/Signup", "signup", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.SignupService/SetupInitialAdmin", "signup-setup", 5},
		{http.MethodPost, "/rpc/tango.identity.v1.OneTimeAccessService/RequestEmail", "one-time-access-email", 2},
		{http.MethodPost, "/rpc/tango.identity.v1.OneTimeAccessService/AdminSendEmail", "one-time-access-email", 2},
		{http.MethodPost, "/api/one-time-access-token/tok_123", "one-time-access-token", 10},
		{http.MethodPost, "/api/device-login/requests", "device-login-create", 10},
		{http.MethodPost, "/api/device-login/requests/dev_1/exchange", "device-login-exchange", 30},
		{http.MethodPost, "/rpc/tango.identity.v1.DeviceApprovalService/GetPendingRequest", "device-login-verify", 10},
		{http.MethodPost, "/rpc/tango.identity.v1.DeviceApprovalService/DecideRequest", "device-login-decision", 10},
		{http.MethodPost, "/api/webauthn/login/finish", "webauthn-login", 10},
		{http.MethodPost, "/rpc/tango.identity.v1.EmailVerificationService/SendEmail", "email-verification-send", 2},
		{http.MethodPost, "/api/users/me/verify-email", "email-verification-verify", 6},
		{http.MethodPost, "/rpc/tango.identity.v1.AccountService/ChangePassword", "account-password", 10},
	}
	for _, tc := range cases {
		policy, ok := PolicyFor(tc.method, tc.path)
		require.True(t, ok, tc.path)
		assert.Equal(t, tc.policy, policy.Name, tc.path)
		assert.Equal(t, tc.max, policy.Max, tc.path)
	}
}

func TestPolicyForIgnoresEverydayRoutes(t *testing.T) {
	cases := []struct {
		method string
		path   string
	}{
		// Admin and user CRUD must never be throttled.
		{http.MethodGet, "/api/users"},
		{http.MethodPut, "/api/users/usr_1"},
		{http.MethodPost, "/api/signup-tokens"},
		{http.MethodGet, "/api/signup-tokens"},
		{http.MethodDelete, "/api/signup-tokens/st_1"},
		{http.MethodGet, "/api/signup/setup"},
		{http.MethodGet, "/api/oidc/clients"},
		{http.MethodPost, "/api/oidc/clients"},
		{http.MethodPut, "/api/oidc/clients/oidc_client_1"},
		{http.MethodGet, "/api/apis"},
		{http.MethodGet, "/api/webhooks"},
		{http.MethodGet, "/api/audit-logs"},
		{http.MethodGet, "/api/application-configuration"},
		{http.MethodPut, "/api/application-configuration"},
		{http.MethodGet, "/api/user-groups"},
		{http.MethodGet, "/api/users/me"},
		{http.MethodPut, "/api/users/me"},
		{http.MethodPut, "/api/account/password"},
		{http.MethodGet, "/api/account/sessions"},
		{http.MethodDelete, "/api/account/sessions/ses_1"},
		{http.MethodPost, "/api/auth/sign-out"},
		{http.MethodGet, "/api/auth/session"},
		{http.MethodPost, "/api/webauthn/login/begin"},
		{http.MethodPost, "/api/webauthn/register/finish"},
		{http.MethodPost, "/api/oidc/token"},
		{http.MethodPost, "/api/oidc/introspect"},
		{http.MethodPost, "/api/oidc/par"},
		{http.MethodPost, "/api/oidc/device/authorize"},
		{http.MethodGet, "/api/oidc/device/info"},
		{http.MethodGet, "/api/oidc/userinfo"},
		{http.MethodGet, "/api/healthz"},
		{http.MethodGet, "/api/version/latest"},
		{http.MethodGet, "/api/users/usr_1/profile-picture.png"},
		// Method mismatches stay unthrottled: a GET on a sensitive path
		// is a routing error, not an attack surface.
		{http.MethodGet, "/rpc/tango.identity.v1.AuthService/SignIn"},
		{http.MethodGet, "/api/one-time-access-email"},
	}
	for _, tc := range cases {
		_, ok := PolicyFor(tc.method, tc.path)
		assert.False(t, ok, "%s %s must not be rate limited", tc.method, tc.path)
	}
}

func TestRateLimitEnforcesPolicyBudgets(t *testing.T) {
	limiter := &fakeLimiter{}
	handler := RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/rpc/tango.identity.v1.AuthService/SignIn", strings.NewReader("")))
	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.Equal(t, 1, limiter.calls)
	assert.Equal(t, 20, limiter.max)
	assert.Equal(t, 60, limiter.window)
	assert.True(t, strings.HasPrefix(limiter.key, "rl_sign-in_"))

	// Unthrottled routes never touch the store.
	limiter = &fakeLimiter{}
	handler = RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	assert.Equal(t, 0, limiter.calls)
}

func TestRateLimitFailOpenOnStoreError(t *testing.T) {
	handler := RateLimit(failingLimiter{err: errors.New("pool down")})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTeapot)
		}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/rpc/tango.identity.v1.AuthService/SignIn", strings.NewReader("")))
	assert.Equal(t, http.StatusTeapot, w.Code, "store failures fail open")
}

func TestRateLimitWrites429WithRetryAfter(t *testing.T) {
	handler := RateLimit(failingLimiter{err: &pgconn.PgError{
		Code:   "42901",
		Detail: "Retry after: 42 seconds",
	}})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("throttled request must not reach the handler")
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/forgot-password", strings.NewReader("")))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "42", w.Header().Get("Retry-After"))
}

func TestRateLimitedResponseUsesEnvelope(t *testing.T) {
	handler := RateLimit(failingLimiter{err: &pgconn.PgError{Code: "42901"}})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("throttled request must not reach the handler")
		}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/rpc/tango.identity.v1.AuthService/SignIn", strings.NewReader("")))
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), "rate limit exceeded")
}
