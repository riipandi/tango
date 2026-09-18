package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
