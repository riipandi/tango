package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/kernel"
)

// next is a trivial terminal handler for middleware tests.
func next(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// TestBearerAuthRejectsAnonymous pins the RPC auth contract: protected
// RPCs require an explicit bearer token; a missing, malformed, or
// empty token yields the Connect unauthenticated error body.
func TestBearerAuthRejectsAnonymous(t *testing.T) {
	handler := BearerAuth(http.HandlerFunc(next))

	cases := []struct {
		name   string
		header string
	}{
		{"no header", ""},
		{"bare scheme", "Bearer"},
		{"empty token", "Bearer "},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"cookie fallback rejected", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/rpc/tango.identity.v1.UserService/ListUsers", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			require.Equal(t, http.StatusUnauthorized, w.Code)
			assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
			assert.JSONEq(t, `{"code":"unauthenticated","message":"bearer token required"}`, w.Body.String())
		})
	}
}

// TestBearerAuthAcceptsToken pins that a well-formed bearer header
// reaches the wrapped handler untouched.
func TestBearerAuthAcceptsToken(t *testing.T) {
	handler := BearerAuth(http.HandlerFunc(next))

	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.identity.v1.UserService/ListUsers", nil)
	req.Header.Set("Authorization", "Bearer internal-access-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// stubAuth resolves any non-empty token to a fixed principal.
type stubAuth struct{ err error }

func (s stubAuth) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if s.err != nil || token == "" {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_1", UserID: "user_1", IsAdmin: true}, nil
}

// TestRPCSessionAuth pins the bearer-only RPC authentication contract:
// valid tokens attach the resolved principal to the request context;
// missing tokens and failed resolution answer distinct Connect
// unauthenticated messages; cookies never authenticate.
func TestRPCSessionAuth(t *testing.T) {
	var seen Principal
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := PrincipalFromContext(r.Context()); ok {
			seen = p
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := RPCSessionAuth(stubAuth{})(probe)

	req := httptest.NewRequest(http.MethodPost, "/rpc/x/Call", nil)
	req.Header.Set("Authorization", "Bearer session-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "user_1", seen.UserID)

	// Cookie fallback rejected even when present.
	req = httptest.NewRequest(http.MethodPost, "/rpc/x/Call", nil)
	req.AddCookie(&http.Cookie{Name: "tango.session", Value: "session-token"})
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "bearer token required")

	// Failed resolution (expired, revoked, unknown) answers the
	// enumeration-safe message.
	handler = RPCSessionAuth(stubAuth{err: errors.New("session: invalid or expired")})(probe)
	req = httptest.NewRequest(http.MethodPost, "/rpc/x/Call", nil)
	req.Header.Set("Authorization", "Bearer anything")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid or expired token")
}
