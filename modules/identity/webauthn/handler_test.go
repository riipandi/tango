package webauthn

// handler_test.go pins the passkey HTTP contract over the real
// session guard: ceremony begins return options with an explicit
// ceremony session id, finishes fail closed on bad input, and the
// admin credential CRUD manages stored passkeys.

import (
	"context"
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

func newTestRouter(t *testing.T) (chi.Router, *user.PostgresStore, *password.Service, *PostgresStore, *Service, *session.Service) {
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

	selfGuard := middleware.RequireAuth(sessions, session.CookieName)
	adminGuard := func(next http.Handler) http.Handler {
		return middleware.RequireAuth(sessions, session.CookieName)(middleware.RequireAdmin(next))
	}

	svc, err := NewService(NewPostgresStore(ds), users,
		func(ctx context.Context, userID user.UserID) (string, error) {
			return sessions.IssueForUser(ctx, userID, "webauthn", session.Meta{})
		}, "http://localhost:3000", nil)
	require.NoError(t, err)
	feature := New(svc)

	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		sessions.APIRoutes(r, identity.RouteGroups{})
		feature.APIRoutes(r, identity.RouteGroups{Self: selfGuard, Admin: adminGuard})
	})
	return r, users, passwords, NewPostgresStore(ds), svc, sessions
}

func signIn(t *testing.T, r chi.Router, users *user.PostgresStore, passwords *password.Service, sessions *session.Service, admin bool) (string, user.User) {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)

	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "key_" + stamp,
		Email:    "key-" + stamp + "@example.com",
		IsAdmin:  admin,
	})
	require.NoError(t, err)
	require.NoError(t, passwords.SetPassword(t.Context(), u.ID, "s3cret-p@ss"))

	result, err := sessions.SignInWithPending(t.Context(), u.Username, "s3cret-p@ss", session.Meta{})
	require.NoError(t, err)
	require.False(t, result.Pending)
	return result.Token, u
}

func TestRegisterBeginRequiresSession(t *testing.T) {
	r, _, _, _, _, _ := newTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/webauthn/register/begin", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRegisterBeginReturnsOptions(t *testing.T) {
	r, users, passwords, _, svc, sessions := newTestRouter(t)
	cookie, u := signIn(t, r, users, passwords, sessions, false)

	// Surface the service error directly: the handler hides it.
	_, _, err := svc.BeginRegistration(t.Context(), u.ID)
	require.NoError(t, err)
	_ = cookie
	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/register/begin", nil)
	req.Header.Set("Cookie", session.CookieName+"="+cookie)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var payload struct {
		PublicKey struct {
			RelyingParty struct {
				ID string `json:"id"`
			} `json:"rp"`
		} `json:"publicKey"`
		SessionID string `json:"session_id"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	assert.NotEmpty(t, payload.SessionID, "begin must return the ceremony session id")
	assert.NotEmpty(t, payload.PublicKey.RelyingParty.ID)
}

func TestLoginBeginAnonymousAndFinishValidation(t *testing.T) {
	r, _, _, _, _, _ := newTestRouter(t)

	// The discoverable login ceremony is anonymous.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/webauthn/login/begin", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var payload struct {
		SessionID string `json:"session_id"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &payload))
	require.NotEmpty(t, payload.SessionID)

	// The finish endpoint fails closed before any crypto: missing
	// ceremony session id, then an unknown one.
	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/login/finish", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "finish without a session id must be rejected")

	req = httptest.NewRequest(http.MethodPost,
		"/api/webauthn/login/finish?session_id=ceremony_unknown", strings.NewReader("{}"))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "an unknown ceremony session must be rejected")
}

func TestCredentialAdminCRUD(t *testing.T) {
	r, users, passwords, store, _, sessions := newTestRouter(t)
	cookie, _ := signIn(t, r, users, passwords, sessions, true)

	// Seed a passkey for another account through the store.
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	member, err := users.Create(t.Context(), user.CreateParams{
		Username: "keymember_" + stamp,
		Email:    "keymember-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	credential := StoredCredential{
		ID:              identity.NewID[CredentialID](),
		UserID:          member.ID,
		Name:            "laptop",
		CredentialID:    []byte("credential-bytes-" + stamp),
		PublicKey:       []byte("public-key-bytes"),
		DeviceType:      "singleDevice",
		AttestationType: "none",
		Transport:       []string{"internal"},
		BackupEligible:  false,
		BackupState:     false,
	}
	require.NoError(t, store.InsertCredential(t.Context(), &credential))

	// The admin lists the member's passkeys.
	w := doGet(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials", cookie)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, jsonv2.Unmarshal(w.Body.Bytes(), &listed))
	require.Len(t, listed.Data, 1)
	assert.Equal(t, "laptop", listed.Data[0]["name"])
	assert.NotContains(t, w.Body.String(), "public-key-bytes", "key material never leaves the store")

	// Rename through the admin surface.
	req := httptest.NewRequest(http.MethodPut,
		"/api/users/"+member.ID.String()+"/webauthn-credentials/"+credential.ID.String(),
		strings.NewReader(`{"name":"travel key"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Delete, then the listing is empty and a repeat is 404.
	w = doDelete(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials/"+credential.ID.String(), cookie)
	assert.Equal(t, http.StatusNoContent, w.Code)

	w = doGet(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials", cookie)
	require.Equal(t, http.StatusOK, w.Code)
	assert.NotContains(t, w.Body.String(), "travel key")

	w = doDelete(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials/"+credential.ID.String(), cookie)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCredentialRoutesAreAdminGated(t *testing.T) {
	r, users, passwords, _, _, sessions := newTestRouter(t)
	cookie, member := signIn(t, r, users, passwords, sessions, false)

	// A signed-in non-admin cannot read another account's passkeys.
	w := doGet(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials", cookie)
	assert.Equal(t, http.StatusForbidden, w.Code)

	// Anonymous requests fail closed too.
	w = doGet(t, r, "/api/users/"+member.ID.String()+"/webauthn-credentials", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func doGet(t *testing.T, r chi.Router, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func doDelete(t *testing.T, r chi.Router, path, cookie string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestRegisterFinishRejectsMissingSession pins the ceremony contract:
// the finish endpoint fails closed before any crypto when the
// ceremony session id is absent.
func TestRegisterFinishRejectsMissingSession(t *testing.T) {
	r, users, passwords, _, _, sessions := newTestRouter(t)
	cookie, _ := signIn(t, r, users, passwords, sessions, false)

	req := httptest.NewRequest(http.MethodPost, "/api/webauthn/register/finish", strings.NewReader("{}"))
	req.AddCookie(&http.Cookie{Name: session.CookieName, Value: cookie})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "finish without a session id must be rejected")
	assert.Contains(t, w.Body.String(), "session_id is required")
}
