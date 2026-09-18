package customclaim

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

// newTestStack builds the full claim stack: real stores, real
// session guard (RequireAuth + RequireAdmin), shared router.
func newTestStack(t *testing.T) (chi.Router, *PostgresStore, *user.PostgresStore, *usergroup.PostgresStore, *password.Service) {
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
	groups := usergroup.NewPostgresStore(ds)
	store := NewPostgresStore(ds)
	passwords := password.NewService(password.NewPostgresStore(ds),
		crypto.NewPasswordHasher().WithAlgorithm(crypto.AlgorithmArgon2id), nil)
	sessions := session.NewService(session.NewPostgresStore(ds), passwords, users, nil)

	adminGuard := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(sessions, session.CookieName)(middleware.RequireAdmin(next))
	}

	svc := NewService(store, nil)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		svc.APIRoutes(r, identity.RouteGroups{Admin: adminGuard})
	})
	return r, store, users, groups, passwords
}

// newUser provisions an admin (guard requires IsAdmin) with credentials.
func newAdminUser(t *testing.T, users *user.PostgresStore, passwords *password.Service, name string) user.User {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: name + "_" + stamp,
		Email:    name + "-" + stamp + "@example.com",
		IsAdmin:  true,
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))
	return u
}

// signIn returns a fresh admin cookie token (default credential).
func signIn(t *testing.T, r chi.Router, u user.User) string {
	return signInWith(t, r, u.Username, "s3cret-p@ss")
}

// signInWith signs in with an explicit secret.
func signInWith(t *testing.T, r chi.Router, identityText, secret string) string {
	t.Helper()
	body := `{"identity":"` + identityText + `","secret":"` + secret + `"}`
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

func TestClaimsForUserAndGroup(t *testing.T) {
	_, store, users, groups, _ := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	u, err := users.Create(ctx, user.CreateParams{
		Username: "claim_u_" + stamp, Email: "claim-u-" + stamp + "@example.com"})
	require.NoError(t, err)
	g, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "claim_g_" + stamp, DisplayName: "Claim Group"})
	require.NoError(t, err)

	// User scope.
	claim, err := store.Create(ctx, UpsertParams{Key: "role", Value: "vip", UserID: strPtr(u.ID.UUID())})
	require.NoError(t, err)
	assert.False(t, claim.ID.IsZero())

	dup, err := store.ExistsForOwner(ctx, UpsertParams{Key: "role", UserID: strPtr(u.ID.UUID())})
	require.NoError(t, err)
	assert.True(t, dup, "existing pair must be detected")

	list, err := store.ListByUser(ctx, u.ID)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, "vip", list[0].Value)

	// Update + delete.
	updated, err := store.Update(ctx, claim.ID, "admin")
	require.NoError(t, err)
	assert.Equal(t, "admin", updated.Value)
	require.NoError(t, store.Delete(ctx, claim.ID))

	// Group scope, same key: independent of the user scope.
	gClaim, err := store.Create(ctx, UpsertParams{Key: "role", Value: "member", UserGroupID: strPtr(g.ID.UUID())})
	require.NoError(t, err)
	gList, err := store.ListByGroup(ctx, g.ID)
	require.NoError(t, err)
	require.Len(t, gList, 1)
	assert.Equal(t, gClaim.ID, gList[0].ID)

	keys, err := store.SuggestedKeys(ctx)
	require.NoError(t, err)
	assert.Contains(t, keys, "role")
}

func TestClaimEndpointsAdminGated(t *testing.T) {
	r, _, users, groups, passwords := newTestStack(t)
	ctx := t.Context()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	admin := newAdminUser(t, users, passwords, "adm")
	member, err := users.Create(ctx, user.CreateParams{
		Username: "member_" + stamp, Email: "member-" + stamp + "@example.com"})
	require.NoError(t, err)
	group, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "ep_g_" + stamp, DisplayName: "Endpoint Group"})
	require.NoError(t, err)
	token := signIn(t, r, admin)

	// Anonymous → 401.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/custom-claims/user/"+member.ID.String(), nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Non-admin member session → 403.
	require.NoError(t, passwords.SetPassword(ctx, member.ID, "member-pass-1"))
	memberToken := signInWith(t, r, member.Username, "member-pass-1")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/custom-claims/user/"+member.ID.String(), memberToken, ""))
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Admin: create → list → update → delete.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPost, "/api/custom-claims/user/"+member.ID.String(), token,
		`{"key":"role","value":"vip"}`))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/custom-claims/user/"+member.ID.String(), token, ""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"value":"vip"`)

	claimID := created.Data.ID
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPut, "/api/custom-claims/user/"+member.ID.String()+"/"+claimID, token,
		`{"value":"admin"}`))
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodDelete, "/api/custom-claims/user/"+member.ID.String()+"/"+claimID, token, ""))
	require.Equal(t, http.StatusOK, w.Code)

	// Duplicate (same owner + key) → 409.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPost, "/api/custom-claims/user-group/"+group.ID.String(), token,
		`{"key":"tier","value":"gold"}`))
	require.Equal(t, http.StatusCreated, w.Code)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodPost, "/api/custom-claims/user-group/"+group.ID.String(), token,
		`{"key":"tier","value":"gold"}`))
	assert.Equal(t, http.StatusConflict, w.Code)

	// Suggestions list the keys in use.
	w = httptest.NewRecorder()
	r.ServeHTTP(w, authed(http.MethodGet, "/api/custom-claims/suggestions", token, ""))
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "tier")
}

func strPtr(s string) *string { return &s }
