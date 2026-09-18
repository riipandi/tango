package account

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// newTestStack builds the full account stack: real stores, real
// password verifier, real session service, shared router.
func newTestStack(t *testing.T) (chi.Router, *Service, *session.Service, *password.Service, user.Store) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	users := user.NewPostgresStore(ds)
	passwords := password.NewService(password.NewPostgresStore(ds),
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmScrypt), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	svc := NewService(users, passwords, sessions, nil)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		svc.APIRoutes(r, identity.RouteGroups{})
	})
	return r, svc, sessions, passwords, users
}

// newUser provisions an account with a known credential.
func newUser(t *testing.T, users user.Store, passwords *password.Service, name string) user.User {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: name + "_" + stamp,
		Email:    name + "-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))
	return u
}

// signIn returns a fresh signed-in token for the user.
func signInToken(t *testing.T, r chi.Router, u user.User) string {
	t.Helper()
	body := `{"identity":"` + u.Username + `","secret":"s3cret-p@ss"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Value
}

func authed(method, path, token, body string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	}
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	return req
}

func TestProfileRoundTrip(t *testing.T) {
	r, _, _, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "prof")
	token := signInToken(t, r, u)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/account", token, ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), u.Username)

	// Anonymous → 401.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/account", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Patch display name + locale.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPatch, "/api/account", token,
		`{"display_name":"Adi P","locale":"id-ID"}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"display_name":"Adi P"`)

	// Empty display name fails validation → 422.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPatch, "/api/account", token,
		`{"display_name":""}`))
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestChangePasswordRevokesOtherSessions(t *testing.T) {
	r, _, _, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "chg")

	tokenA := signInToken(t, r, u) // stays alive (current during change)
	tokenB := signInToken(t, r, u) // dies with the rotation
	tokenC := signInToken(t, r, u) // dies too

	// Wrong current password → 400.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPut, "/api/account/password", tokenA,
		`{"current_password":"wrong","new_password":"new-pass-99"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Correct rotation from session A: B and C die, A survives.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPut, "/api/account/password", tokenA,
		`{"current_password":"s3cret-p@ss","new_password":"new-pass-99"}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/auth/session", tokenB, ""))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "session B must be revoked")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/auth/session", tokenC, ""))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "session C must be revoked")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/auth/session", tokenA, ""))
	assert.Equal(t, http.StatusOK, w.Code, "current session survives")

	// Weak new password → 422.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPut, "/api/account/password", tokenA,
		`{"current_password":"new-pass-99","new_password":"short"}`))
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestSessionListAndRevoke(t *testing.T) {
	r, _, _, passwords, users := newTestStack(t)
	u := newUser(t, users, passwords, "sls")

	tokenA := signInToken(t, r, u)
	tokenB := signInToken(t, r, u)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/account/sessions", tokenA, ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body struct {
		Data []struct {
			ID         string `json:"id"`
			TokenHash  string `json:"token_hash"`
			UserAgent  string `json:"user_agent"`
			IPAdress   string `json:"ip_address"`
			DeviceName string `json:"device_name"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)
	for _, s := range body.Data {
		assert.True(t, strings.HasPrefix(s.ID, "session_"))
		assert.Empty(t, s.TokenHash, "token hash must never be exposed")
	}

	// Revoke session B by ID using session A.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodDelete, "/api/account/sessions/"+body.Data[0].ID, tokenB, ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Unknown ID → 404.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodDelete, "/api/account/sessions/session_01j00000000000000000000000", tokenA, ""))
	assert.Equal(t, http.StatusNotFound, w.Code)
}
