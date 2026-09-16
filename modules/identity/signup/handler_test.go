package signup

// handler_test.go pins the signup HTTP contract: the setup-available
// probe, the first-admin setup rejection rules, and the token-gated
// signup + admin token CRUD, all over the real session guard.
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
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

func newTestRouter(t *testing.T) (chi.Router, *Service, *user.PostgresStore, *password.Service, datastore.Store) {
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

	svc := NewService(NewPostgresStore(ds), users, usergroup.NewPostgresStore(ds), sessions, nil)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		New(svc).APIRoutes(r, identity.RouteGroups{Admin: adminGuard})
	})
	return r, svc, users, passwords, ds
}

// signInAdmin provisions an admin with credentials and returns its
// session cookie value from the sign-in route.
func signInAdmin(t *testing.T, r chi.Router, users *user.PostgresStore, passwords *password.Service) string {
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
	return cookies[0].Value
}

func doJSON(t *testing.T, r chi.Router, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSetupAvailableLifecycle(t *testing.T) {
	r, _, _, _, _ := newTestRouter(t)

	// A fresh database can run the initial setup: 204, then the
	// first admin is created and issues a session.
	w := doJSON(t, r, http.MethodGet, "/api/signup/setup", "")
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = doJSON(t, r, http.MethodPost, "/api/signup/setup",
		`{"username":"root_admin","email":"root@example.com"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Every later probe reports the setup as gone and a repeated
	// setup conflicts.
	w = doJSON(t, r, http.MethodGet, "/api/signup/setup", "")
	assert.Equal(t, http.StatusNotFound, w.Code)

	w = doJSON(t, r, http.MethodPost, "/api/signup/setup",
		`{"username":"second_admin","email":"second@example.com"}`)
	assert.Equal(t, http.StatusConflict, w.Code)
}

// TestSignupRequiresValidToken
func TestSignupRequiresValidToken(t *testing.T) {
	r, svc, _, _, _ := newTestRouter(t)

	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	// An unknown token is not found.
	w := doJSON(t, r, http.MethodPost, "/api/signup",
		`{"username":"tk_user_`+stamp+`","email":"tk-`+stamp+`@example.com","token":"nope"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	token, raw, err := svc.CreateToken(t.Context(), CreateParams{UsageLimit: 1})
	require.NoError(t, err)
	require.NotEmpty(t, raw)

	w = doJSON(t, r, http.MethodPost, "/api/signup",
		`{"username":"tk_user_`+stamp+`","email":"tk-`+stamp+`@example.com","token":"`+raw+`"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created struct {
		Data struct {
			Username string `json:"username"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))
	assert.Equal(t, "tk_user_"+stamp, created.Data.Username)

	// The token was single-use: a second signup no longer finds a
	// valid token.
	w = doJSON(t, r, http.MethodPost, "/api/signup",
		`{"username":"tk_other_`+stamp+`","email":"other-`+stamp+`@example.com","token":"`+raw+`"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	require.NoError(t, svc.DeleteToken(t.Context(), token.ID))
}

func TestSignupTokenAdminCRUD(t *testing.T) {
	r, _, users, passwords, _ := newTestRouter(t)
	cookie := signInAdmin(t, r, users, passwords)

	// Create with a bare-body admin session.
	req := httptest.NewRequest(http.MethodPost, "/api/signup-tokens", strings.NewReader(`{"usage_limit":2}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created struct {
		Data map[string]any `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))
	require.Contains(t, created.Data, "token", "the raw token is shown once at creation")

	// The listing shows the token without the raw secret.
	req = httptest.NewRequest(http.MethodGet, "/api/signup-tokens", nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), `"token":"`)

	// Deleting removes it from the listing.
	id, _ := created.Data["id"].(string)
	require.NotEmpty(t, id)
	req = httptest.NewRequest(http.MethodDelete, "/api/signup-tokens/"+id, nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	req = httptest.NewRequest(http.MethodGet, "/api/signup-tokens", nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.NotContains(t, w.Body.String(), id)
}

// TestSignupTokenRoutesAreAdminGated
func TestSignupTokenRoutesAreAdminGated(t *testing.T) {
	r, _, _, _, _ := newTestRouter(t)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/signup-tokens"},
		{http.MethodPost, "/api/signup-tokens"},
		{http.MethodDelete, "/api/signup-tokens/signup_01abc"},
	} {
		w := doJSON(t, r, tc.method, tc.path, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code, tc.path)
	}
}
