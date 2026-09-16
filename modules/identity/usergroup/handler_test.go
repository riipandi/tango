package usergroup

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
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// newTestRouter mounts the group routes behind the real session
// guard (RequireAuth + RequireAdmin), like production.
func newTestRouter(t *testing.T) (chi.Router, *user.PostgresStore, *PostgresStore, *password.Service, datastore.Store) {
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

	svc := NewService(NewPostgresStore(ds), nil)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		svc.APIRoutes(r, identity.RouteGroups{Admin: adminGuard})
	})
	return r, users, NewPostgresStore(ds), passwords, ds
}

// newAdminUser provisions an admin with credentials.
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

// TestReplaceUserGroupsForUser drives PUT /users/{id}/user-groups:
// the payload replaces the user's group set, unknown group IDs are
// rejected, and the response lists the assigned groups.
func TestReplaceUserGroupsForUser(t *testing.T) {
	r, users, groups, passwords, _ := newTestRouter(t)
	admin := newAdminUser(t, users, passwords, "ugadm")
	cookie := signInToken(t, r, admin)

	member := newAdminUser(t, users, passwords, "member")
	ugSvc := NewService(groups, nil)

	create := func(name string) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/user-groups",
			strings.NewReader(`{"name":"`+name+`","display_name":"`+name+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var created struct {
			Data struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))
		return created.Data.ID
	}
	engineering := create("engineering_" + strconv.FormatInt(time.Now().UnixNano(), 10))
	ops := create("ops_" + strconv.FormatInt(time.Now().UnixNano(), 10))

	// Assign both groups; the response mirrors the new set.
	req := httptest.NewRequest(http.MethodPut, "/api/users/"+member.ID.String()+"/user-groups",
		strings.NewReader(`{"user_group_ids":["`+engineering+`","`+ops+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	memberships, err := ugSvc.GroupsForUser(t.Context(), member.ID)
	require.NoError(t, err)
	assert.Len(t, memberships, 2)

	// Replacing with one group drops the other.
	req = httptest.NewRequest(http.MethodPut, "/api/users/"+member.ID.String()+"/user-groups",
		strings.NewReader(`{"user_group_ids":["`+ops+`"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	memberships, err = ugSvc.GroupsForUser(t.Context(), member.ID)
	require.NoError(t, err)
	assert.Len(t, memberships, 1)

	// An unknown group ID is a 422, not a partial write.
	req = httptest.NewRequest(http.MethodPut, "/api/users/"+member.ID.String()+"/user-groups",
		strings.NewReader(`{"user_group_ids":["user_group_01zzzzzzzzzzzzzzzzzzzzzz"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	memberships, err = ugSvc.GroupsForUser(t.Context(), member.ID)
	require.NoError(t, err)
	assert.Len(t, memberships, 1, "a failed replace must not change the set")
}

func TestGroupEndpointsAdminGated(t *testing.T) {	r, users, _, passwords, _ := newTestRouter(t)
	admin := newAdminUser(t, users, passwords, "gadm")
	token := signInToken(t, r, admin)

	// Anonymous → 401.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/user-groups", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Create → list → get with members.
	w = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/user-groups",
		strings.NewReader(`{"name":"engineering","display_name":"Engineering"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))
	groupID := created.Data.ID
	require.True(t, strings.HasPrefix(groupID, "user_group_"))

	// Duplicate name → 409.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user-groups",
		strings.NewReader(`{"name":"engineering","display_name":"Again"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Validation failure → 422.
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/user-groups",
		strings.NewReader(`{"name":"","display_name":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// Set members with an unknown ID → 422 (FK-rolled-back tx).
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/user-groups/"+groupID+"/users",
		strings.NewReader(`{"user_ids":["user_01j00000000000000000000000"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// Delete → group gone (404 on re-read).
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/user-groups/"+groupID, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/user-groups/"+groupID, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestSetAllowedClients covers PUT /user-groups/{id}/allowed-oidc-clients:
// list-replace with unknown-id refusal and echo-back of the stored list.
func TestSetAllowedClients(t *testing.T) {
	r, users, _, passwords, ds := newTestRouter(t)
	ctx := t.Context()

	admin := newAdminUser(t, users, passwords, "goidc")
	token := signInToken(t, r, admin)
	cookie := func(req *http.Request) {
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: token})
	}

	// Create a group.
	req := httptest.NewRequest(http.MethodPost, "/api/user-groups",
		strings.NewReader(`{"name":"clients","display_name":"Clients"}`))
	cookie(req)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	var created struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &created))
	groupID := created.Data.ID

	// Two relying-party client rows as FK targets (id is a typeid
	// string in a TEXT column).
	clientID := "oidc_client_" + strings.ToLower(strconv.FormatInt(time.Now().UnixNano(), 10)) + "abcdef"
	_, err := ds.Exec(ctx, `INSERT INTO public.oidc_clients (id, name) VALUES ($1, 'RP')`, clientID)
	require.NoError(t, err)

	// Replace with one known client → echo.
	req = httptest.NewRequest(http.MethodPut, "/api/user-groups/"+groupID+"/allowed-oidc-clients",
		strings.NewReader(`{"oidc_client_ids":["`+clientID+`"]}`))
	cookie(req)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var listed struct {
		Data struct {
			OidcClientIDs []string `json:"oidc_client_ids"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &listed))
	assert.Equal(t, []string{clientID}, listed.Data.OidcClientIDs)

	// Unknown client → 422.
	req = httptest.NewRequest(http.MethodPut, "/api/user-groups/"+groupID+"/allowed-oidc-clients",
		strings.NewReader(`{"oidc_client_ids":["oidc_client_01j00000000000000000000000"]}`))
	cookie(req)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)

	// Empty list clears the allowlist.
	req = httptest.NewRequest(http.MethodPut, "/api/user-groups/"+groupID+"/allowed-oidc-clients",
		strings.NewReader(`{"oidc_client_ids":[]}`))
	cookie(req)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var cleared struct {
		Data struct {
			OidcClientIDs []string `json:"oidc_client_ids"`
		} `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &cleared))
	assert.Empty(t, cleared.Data.OidcClientIDs)
}
