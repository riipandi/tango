package middleware

// ratelimit_test.go pins the path-to-class mapping and the budgets:
// sensitive auth surfaces share the tight budget, the rest the
// default one.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
)

// fakeLimiter records the budget it was called with.
type fakeLimiter struct {
	key    string
	max    int
	window int
}

func (f *fakeLimiter) QueryRow(ctx context.Context, query string, args ...any) pgx.Row {
	f.key = args[0].(string)
	f.max = args[1].(int)
	f.window = args[2].(int)
	return fakeRow{payload: []byte(`{"limit":1,"remaining":1,"reset":0}`)}
}

// fakeRow satisfies pgx.Row with a fixed JSON result.
type fakeRow struct{ payload []byte }

func (r fakeRow) Scan(dest ...any) error {
	*(dest[0].(*[]byte)) = r.payload
	return nil
}

func TestClassForPath(t *testing.T) {
	for _, path := range []string{
		"/api/auth/sign-in",
		"/api/account/password",
		"/api/mfa/totp/verify",
		"/api/oidc/token",
		"/api/signup",
		"/api/signup-tokens",
		"/api/one-time-access-email",
		"/api/one-time-access-token/abc",
		"/api/device-login/requests",
		"/api/webauthn/login/begin",
		"/api/users/me/send-email-verification",
		"/api/users/me/verify-email",
	} {
		assert.Equal(t, RateClassAuth, ClassForPath(path), path)
	}

	for _, path := range []string{
		"/api/users",
		"/api/oidc/clients",
		"/api/apis",
		"/api/version/current",
		"/healthz",
	} {
		assert.Equal(t, RateClassDefault, ClassForPath(path), path)
	}
}

func TestRateLimitUsesPathClassBudgets(t *testing.T) {
	limiter := &fakeLimiter{}
	handler := RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/oidc/token", strings.NewReader("")))
	assert.Equal(t, http.StatusTeapot, w.Code)
	assert.Equal(t, 20, limiter.max, "auth surfaces get the tight budget")
	assert.Equal(t, 60, limiter.window)
	assert.True(t, strings.HasPrefix(limiter.key, "rl_"+RateClassAuth+"_"))

	limiter = &fakeLimiter{}
	handler = RateLimit(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/users", nil))
	assert.Equal(t, 100, limiter.max, "ordinary routes get the default budget")
	assert.Equal(t, 900, limiter.window)
}
