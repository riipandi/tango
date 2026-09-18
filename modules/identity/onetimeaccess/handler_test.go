package onetimeaccess

// handler_test.go pins the one-time access HTTP contract: the admin
// minting endpoints, the anti-enumeration email probe, and the
// token-to-session exchange, over the real session guard.

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
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

func newTestRouter(t *testing.T) (chi.Router, *user.PostgresStore, *password.Service) {
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
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmArgon2id), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	adminGuard := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(sessions, session.CookieName)(middleware.RequireAdmin(next))
	}

	svc := NewService(token.NewStore(ds, token.PurposeOneTimeAccess), users, sessions, nil)
	feature := New(svc).WithCookie(session.CookieName, false)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		feature.APIRoutes(r, identity.RouteGroups{Admin: adminGuard})
	})
	return r, users, passwords
}

func doJSON(t *testing.T, r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func adminCookie(t *testing.T, r chi.Router, users *user.PostgresStore, passwords *password.Service) (string, user.User) {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "admin_" + stamp,
		Email:    "admin-" + stamp + "@example.com",
		IsAdmin:  true,
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))

	req := httptest.NewRequest(http.MethodPost, "/api/auth/sign-in",
		strings.NewReader(`{"identity":"admin_`+stamp+`","secret":"s3cret-p@ss"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies)
	return cookies[0].Value, u
}

func TestAdminMintAndExchange(t *testing.T) {
	r, users, passwords := newTestRouter(t)
	cookie, _ := adminCookie(t, r, users, passwords)

	// The admin mints a token for another account; the raw value is
	// shown once.
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	target, err := users.Create(t.Context(), user.CreateParams{
		Username: "member_" + stamp,
		Email:    "member-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/users/"+target.ID.String()+"/one-time-access-token", nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var minted struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &minted))
	require.NotEmpty(t, minted.Data.Token)

	// The exchange consumes the token and sets the session cookie.
	w = doJSON(t, r, http.MethodPost, "/api/one-time-access-token/"+minted.Data.Token, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cookies := w.Result().Cookies()
	require.NotEmpty(t, cookies, "the exchange must issue the session cookie")
	assert.False(t, cookies[0].Secure, "the test cookie wiring is insecure-by-config")

	// The token is burned: a replay is not found.
	w = doJSON(t, r, http.MethodPost, "/api/one-time-access-token/"+minted.Data.Token, "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestAdminEmailMint(t *testing.T) {
	r, users, passwords := newTestRouter(t)
	cookie, _ := adminCookie(t, r, users, passwords)

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	target, err := users.Create(t.Context(), user.CreateParams{
		Username: "member_" + stamp,
		Email:    "member-" + stamp + "@example.com",
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/users/"+target.ID.String()+"/one-time-access-email", nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestEmailRequestNeverEnumerates(t *testing.T) {
	r, _, _ := newTestRouter(t)

	// The unauthenticated probe answers 204 regardless of whether the
	// address exists (the minting policy lives in appconfig).
	w := doJSON(t, r, http.MethodPost, "/api/one-time-access-email",
		`{"email":"nobody-`+strconv.FormatInt(time.Now().UnixNano(), 10)+`@example.com"}`)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// An invalid body is rejected before any policy runs.
	w = doJSON(t, r, http.MethodPost, "/api/one-time-access-email", `{}`)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
}

func TestExchangeUnknownToken(t *testing.T) {
	r, _, _ := newTestRouter(t)

	w := doJSON(t, r, http.MethodPost, "/api/one-time-access-token/deadbeef", "")
	assert.Equal(t, http.StatusNotFound, w.Code)
}
