package user

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to a principal; tokens with
// the "anon-" prefix resolve non-admin and "revoked-" fails, standing
// in for the guard branches. The user id is a real TypeID so the
// self-service handlers can parse it.
type stubAccess struct {
	fail   bool
	userID string
}

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if s.fail || token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	principal := kernel.Principal{SessionID: "sess_test", UserID: s.userID, Username: "tester"}
	if !strings.HasPrefix(token, "anon-") {
		principal.IsAdmin = true
	}
	return principal, nil
}

// rpcRouter mounts the Connect user surface behind the real guard
// with the stub authenticator; userID backs the self procedures.
func rpcRouter(t *testing.T, svc *Service, userID string) http.Handler {
	t.Helper()
	prefix, handler := svc.RPCService(stubAccess{userID: userID})
	mux := http.NewServeMux()
	mux.Handle(prefix, handler)
	return mux
}

// rpcPost posts a procedure with the given bearer.
func rpcPost(t *testing.T, h http.Handler, procedure, bearer, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.UserService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// adminSeed provisions an admin principal's user row.
func adminSeed(t *testing.T, store *PostgresStore) User {
	t.Helper()
	u, err := store.Create(t.Context(), CreateParams{
		Username: "rpc_" + uniqueStamp(),
		Email:    "rpc-" + uniqueStamp() + "@example.com",
	})
	require.NoError(t, err)
	return u
}

// TestUserRPCAdminLifecycle walks create → list → get → update →
// delete through the real transport with an admin bearer.
func TestUserRPCAdminLifecycle(t *testing.T) {
	_, svc, store, selfID := newPictureStack(t, false)
	h := rpcRouter(t, svc, selfID.String())

	// Create → the generated User view.
	w := rpcPost(t, h, "CreateUser", "admin-1", `{"username":"`+"rpc_lf_"+uniqueStamp()+`","email":"`+"lf-"+uniqueStamp()+`@example.com"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"display_name"`)

	// ListUsers echoes pagination metadata.
	w = rpcPost(t, h, "ListUsers", "admin-1", `{"page":1,"limit":10}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"metadata"`)

	// Validation failure → invalid_argument with field text.
	w = rpcPost(t, h, "CreateUser", "admin-1", `{"username":"x","email":"nope"}`)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid_argument")

	// Duplicate username → already_exists.
	username := "rpc_dup_" + uniqueStamp()
	payload := `{"username":"` + username + `","email":"` + username + `@example.com"}`
	require.Equal(t, http.StatusOK, rpcPost(t, h, "CreateUser", "admin-1", payload).Code)
	w = rpcPost(t, h, "CreateUser", "admin-1", `{"username":"`+strings.ToUpper(username)+`","email":"`+username+`-2@example.com"}`)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Unknown TypeID shape → not_found (raw UUIDs 404 too).
	w = rpcPost(t, h, "GetUser", "admin-1", `{"user_id":"user_00000000000000000000000000"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Update then delete the seeded admin.
	u := adminSeed(t, store)
	w = rpcPost(t, h, "UpdateUser", "admin-1", `{"user_id":"`+u.ID.String()+`","display_name":"Renamed"}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Renamed")

	w = rpcPost(t, h, "DeleteUser", "admin-1", `{"user_id":"`+u.ID.String()+`"}`)
	require.Equal(t, http.StatusOK, w.Code)
	_, err := store.GetByID(t.Context(), u.ID)
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestUserRPCSelfBranches pins the mixed visibility contract: the
// self profile procedures answer any principal, admin CRUD denies
// non-admins with permission_denied, and anonymous calls answer
// unauthenticated before the handler runs.
func TestUserRPCSelfBranches(t *testing.T) {
	_, svc, _, selfID := newPictureStack(t, false)
	h := rpcRouter(t, svc, selfID.String())

	// Anonymous UpdateMe → the guard rejects before the handler.
	w := rpcPost(t, h, "UpdateMe", "", `{"display_name":"Me"}`)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Non-admin principal passes self procedures…
	w = rpcPost(t, h, "UpdateMe", "anon-1", `{"display_name":"Me"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// …but is denied admin CRUD.
	w = rpcPost(t, h, "ListUsers", "anon-1", `{"page":1,"limit":10}`)
	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "permission_denied")
}

// TestUserRPCPictureBranches covers the bytes-based picture surface:
// admin writes on a target, self writes on the caller, garbage bytes
// answer invalid_argument.
func TestUserRPCPictureBranches(t *testing.T) {
	_, svc, store, _ := newPictureStack(t, false)
	u := adminSeed(t, store)
	h := rpcRouter(t, svc, u.ID.String())

	image := `{"image":"` + rpcBase64(t, tinyPNG) + `"}`
	w := rpcPost(t, h, "UpdateProfilePicture", "admin-1", `{"user_id":"`+u.ID.String()+`","image":"`+rpcBase64(t, tinyPNG)+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = rpcPost(t, h, "UpdateMyProfilePicture", "admin-1", image)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = rpcPost(t, h, "UpdateMyProfilePicture", "admin-1", `{"image":"bm9wZQ=="}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	w = rpcPost(t, h, "DeleteMyProfilePicture", "admin-1", "{}")
	assert.Equal(t, http.StatusOK, w.Code)

	_ = store
}

func rpcBase64(t *testing.T, raw []byte) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(raw)
}
