package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/pkg/responder"
)

// fakeAuthenticator maps one token to one principal.
type fakeAuthenticator struct {
	token     string
	principal Principal
	err       error
}

func (f fakeAuthenticator) ResolveSession(_ context.Context, token string) (Principal, error) {
	if token != f.token {
		return Principal{}, errors.New("nope")
	}
	return f.principal, f.err
}

// probe writes the context principal to a response header.
var probe = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	p, ok := PrincipalFromContext(r.Context())
	switch {
	case !ok:
		w.Header().Set("X-Principal", "none")
	case p.IsAdmin:
		w.Header().Set("X-Principal", "admin:"+p.UserID)
	default:
		w.Header().Set("X-Principal", "user:"+p.UserID)
	}
	w.WriteHeader(http.StatusOK)
})

func doReq(handler http.Handler, cookie *http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestRequireAuthResolvesPrincipal(t *testing.T) {
	auth := fakeAuthenticator{token: "tok", principal: Principal{UserID: "u1", IsAdmin: true}}
	handler := RequireAuth(auth, "sid")(probe)

	w := doReq(handler, &http.Cookie{Name: "sid", Value: "tok"})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "admin:u1", w.Header().Get("X-Principal"))
}

func TestRequireAuthRejectsAnonymous(t *testing.T) {
	auth := fakeAuthenticator{token: "tok"}
	handler := RequireAuth(auth, "sid")(probe)

	w := doReq(handler, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Unknown tokens use the same status.
	w = doReq(handler, &http.Cookie{Name: "sid", Value: "wrong"})
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRequireAdmin(t *testing.T) {
	admin := fakeAuthenticator{token: "adm", principal: Principal{UserID: "a1", IsAdmin: true}}
	member := fakeAuthenticator{token: "usr", principal: Principal{UserID: "m1"}}

	guarded := RequireAdmin(probe)

	// Admins pass; members are forbidden.
	adminRoute := RequireAuth(admin, "sid")(guarded)
	w := doReq(adminRoute, &http.Cookie{Name: "sid", Value: "adm"})
	assert.Equal(t, http.StatusOK, w.Code)

	memberRoute := RequireAuth(member, "sid")(guarded)
	w = doReq(memberRoute, &http.Cookie{Name: "sid", Value: "usr"})
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Missing principals return 401, not 403.
	w = doReq(guarded, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// The 401 body uses the standard envelope.
func TestRequireAuthEnvelopeShape(t *testing.T) {
	handler := RequireAuth(fakeAuthenticator{token: "x"}, "sid")(probe)

	w := doReq(handler, nil)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Body.String(), responder.StatusError)
}
