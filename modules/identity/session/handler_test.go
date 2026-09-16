package session

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
)

// newTestRouter mounts the real session routes over the shared test
// container. Returns the router and the services for provisioning.
func newTestRouter(t *testing.T, opts ...ServiceOption) (chi.Router, *Service, *password.Service, user.Store) {
	sessions, passwords, users := newTestStack(t, opts...)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
	})
	return r, sessions, passwords, users
}

// signIn submits the form and returns the recorder with the raw
// session cookie value from Set-Cookie.
func signIn(t *testing.T, r chi.Router, identity, secret string) *httptest.ResponseRecorder {
	t.Helper()

	body := `{"identity":"` + identity + `","secret":"` + secret + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// sessionCookie extracts the session token from a sign-in response.
func sessionCookie(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Len(t, w.Result().Cookies(), 1, "sign-in must set exactly one cookie")
	cookie := w.Result().Cookies()[0]
	require.Equal(t, CookieName, cookie.Name)
	assert.True(t, cookie.HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookie.SameSite)
	assert.Equal(t, "/", cookie.Path, "the session cookie must span the app")
	assert.False(t, cookie.Expires.IsZero(), "the session cookie carries its expiry")
	return cookie.Value
}

func TestSignInSessionSignOutRoundTrip(t *testing.T) {
	r, _, passwords, users := newTestRouter(t)
	u := newUser(t, users, passwords, "rt")

	// Anonymous session probe → 401.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/auth/session", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Sign-in → 200 + cookie.
	w = signIn(t, r, u.Username, "s3cret-p@ss")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	token := sessionCookie(t, w)

	// Cookie round trip → live session.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Status string `json:"status"`
		Data   struct {
			SessionID string `json:"session_id"`
			User      struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"user"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "success", body.Status)
	assert.Equal(t, u.Username, body.Data.User.Username)
	assert.True(t, strings.HasPrefix(body.Data.SessionID, "session_"))

	// Sign-out clears the cookie and kills the token.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/auth/sign-out", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// The old token is dead.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: CookieName, Value: token})
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestSignInRejectsBadCredentials(t *testing.T) {
	r, _, passwords, users := newTestRouter(t)
	u := newUser(t, users, passwords, "bad")

	w := signIn(t, r, u.Username, "wrong-secret")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Empty(t, w.Result().Cookies())

	// Unknown identity, same response (no account enumeration).
	w = signIn(t, r, "ghost", "whatever")
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// A disabled account answers the same generic failure: the
	// secret may be correct but nothing leaks.
	_, err := users.UpdateAdmin(t.Context(), u.ID, user.AdminUpdateParams{Disabled: boolPtr(true)})
	require.NoError(t, err)
	w = signIn(t, r, u.Username, "s3cret-p@ss")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.NotContains(t, w.Body.String(), "disabled")
	assert.Empty(t, w.Result().Cookies())
}

// boolPtr sugar for admin patches.
func boolPtr(v bool) *bool { return &v }

// recorderFunc adapts a function to the audit Recorder contract.
type recorderFunc func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor)

func (f recorderFunc) Record(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
	f(ctx, e, exec)
}

// TestSignInAuditsEvents pins the audit trail: a successful sign-in
// records user.signed_in with the session as target.
func TestSignInAuditsEvents(t *testing.T) {
	var events []identity.AuditEvent
	recorder := recorderFunc(func(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
		events = append(events, e)
	})

	r, _, passwords, users := newTestRouter(t, WithRecorder(recorder))
	u := newUser(t, users, passwords, "audit")

	w := signIn(t, r, u.Username, "s3cret-p@ss")
	require.Equal(t, http.StatusOK, w.Code)

	require.Len(t, events, 1)
	assert.Equal(t, "user.signed_in", events[0].Action)
	assert.Equal(t, u.ID.String(), events[0].Actor)
}

func TestSignInValidatesPayload(t *testing.T) {
	r, _, _, _ := newTestRouter(t)

	w := signIn(t, r, "", "")
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", strings.NewReader(`{oops`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}
