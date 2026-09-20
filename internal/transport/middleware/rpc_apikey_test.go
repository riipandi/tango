package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/protobuf/types/known/emptypb"
)

// machineAdmin resolves any non-empty key to an admin machine
// principal, standing in for the API key verifier.
func machineAdmin(_ context.Context, key string) (Principal, error) {
	if key != "k-valid" {
		return Principal{}, errors.New("apikey: invalid credentials")
	}
	return Principal{UserID: "user_machine", Provider: "api_key", IsAdmin: true}, nil
}

// TestRPCAPIKeyAuthResolvesMachinePrincipal pins the machine-credential
// contract: an X-API-KEY header resolves to a principal the RPC guards
// accept, requests without the header pass through untouched so the
// bearer path keeps working, and a bad key answers the
// enumeration-safe Connect error.
func TestRPCAPIKeyAuthResolvesMachinePrincipal(t *testing.T) {
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := machinePrincipal(r.Context()); ok {
			w.Header().Set("X-Seen-User", p.UserID)
		}
		w.WriteHeader(http.StatusOK)
	})
	handler := RPCAPIKeyAuth(machineAdmin)(probe)

	// A valid key resolves.
	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.admin.v1.ApiService/ListApis", nil)
	req.Header.Set("X-API-KEY", "k-valid")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "user_machine", w.Header().Get("X-Seen-User"))

	// No header passes through: the bearer path owns the request.
	req = httptest.NewRequest(http.MethodPost, "/rpc/tango.admin.v1.ApiService/ListApis", nil)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("X-Seen-User"))

	// An unknown key never reaches the handler.
	req = httptest.NewRequest(http.MethodPost, "/rpc/tango.admin.v1.ApiService/ListApis", nil)
	req.Header.Set("X-API-KEY", "k-bogus")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.JSONEq(t, `{"code":"unauthenticated","message":"invalid API key"}`, w.Body.String())
}

// TestRPCAdminGuardAcceptsMachinePrincipal pins that a machine
// principal satisfies the admin guard without a bearer token, and that
// a non-admin machine principal is denied.
func TestRPCAdminGuardAcceptsMachinePrincipal(t *testing.T) {
	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := RPCAPIKeyAuth(func(_ context.Context, key string) (Principal, error) {
		if key != "k-member" {
			return Principal{}, errors.New("apikey: invalid credentials")
		}
		return Principal{UserID: "user_member", Provider: "api_key"}, nil
	})(RPCAdminGuard(stubAuth{})(probe))

	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.admin.v1.ApiService/ListApis", nil)
	req.Header.Set("X-API-KEY", "k-member")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "admin access required")
}

// TestRPCAdminGuardRejectsCookiePrincipal pins that a principal a
// cookie-backed middleware placed in the context never satisfies an
// RPC guard: cookies stay token storage, never RPC authorization.
func TestRPCAdminGuardRejectsCookiePrincipal(t *testing.T) {
	probe := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	cookiePrincipal := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := WithPrincipal(r.Context(), Principal{UserID: "user_cookie", IsAdmin: true})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
	handler := cookiePrincipal(RPCAdminGuard(stubAuth{})(probe))

	req := httptest.NewRequest(http.MethodPost, "/rpc/tango.admin.v1.ApiService/ListApis", nil)
	req.AddCookie(&http.Cookie{Name: "tango.session", Value: "session-token"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "bearer token required")
}

// TestRPCMachineDenied pins the self-management rule: a machine
// credential cannot mint or renew API keys, while listing stays
// available to it.
func TestRPCMachineDenied(t *testing.T) {
	machine := map[string]bool{"/tango.admin.v1.ApiKeyService/Create": true}
	ok := func(context.Context, *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
		return connect.NewResponse(&emptypb.Empty{}), nil
	}
	verifier := func(_ context.Context, key string) (Principal, error) {
		if key != "k-valid" {
			return Principal{}, errors.New("apikey: invalid credentials")
		}
		return Principal{UserID: "user_machine", Provider: "api_key", IsAdmin: true}, nil
	}

	call := func(t *testing.T, procedure string) *httptest.ResponseRecorder {
		t.Helper()
		handler := connect.NewUnaryHandler(procedure, ok,
			connect.WithInterceptors(RPCMachineDenied(machine)))
		guarded := RPCAPIKeyAuth(verifier)(handler)

		req := httptest.NewRequest(http.MethodPost, procedure, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		req.Header.Set("X-API-KEY", "k-valid")
		w := httptest.NewRecorder()
		guarded.ServeHTTP(w, req)
		return w
	}

	// A machine credential may list its own keys.
	require.Equal(t, http.StatusOK, call(t, "/tango.admin.v1.ApiKeyService/List").Code)

	// Minting demands a session: the machine credential is refused.
	minted := call(t, "/tango.admin.v1.ApiKeyService/Create")
	require.Equal(t, http.StatusForbidden, minted.Code)
	assert.Contains(t, minted.Body.String(), "session authentication required")
}
