package usergroup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAccess resolves any bearer token to a principal; "revoked-"
// fails and other tokens are admin.
type stubAccess struct{ userID string }

func (s stubAccess) ResolveAccess(_ context.Context, token string) (kernel.Principal, error) {
	if token == "" || strings.HasPrefix(token, "revoked-") {
		return kernel.Principal{}, errors.New("session: invalid or expired")
	}
	return kernel.Principal{SessionID: "sess_test", UserID: s.userID, IsAdmin: true}, nil
}

// newRPCStack builds the group service over the throwaway database
// with the member projection wired, and mounts the Connect surface
// behind the admin guard.
func newRPCStack(t *testing.T) (http.Handler, *Service, user.Store) {
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
	svc := NewService(NewPostgresStore(ds), nil, WithUserStore(users))

	_, handler := svc.RPCService()
	mux := http.NewServeMux()
	mux.Handle("/tango.identity.v1.UserGroupService/", middleware.RPCAdminGuard(stubAccess{userID: userTypeID(t, users)})(handler))
	return mux, svc, users
}

// userTypeID provisions one user and returns its TypeID.
func userTypeID(t *testing.T, users user.Store) string {
	t.Helper()
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: "grp_" + stamp(),
		Email:    "grp-" + stamp() + "@example.com",
	})
	require.NoError(t, err)
	return u.ID.String()
}

// stamp yields a per-call unique suffix.
func stamp() string {
	return strings.ReplaceAll(time.Now().Format("150405.000000000"), ".", "")
}

// rpcPost posts a group procedure with the admin bearer.
func rpcPost(t *testing.T, h http.Handler, procedure, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.UserGroupService/"+procedure, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Authorization", "Bearer admin-1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// TestGroupRPCAdminCRUD walks the whole group surface through the
// real transport: create, list, get, update, membership replace and
// listing, the OIDC allowlist, and delete.
func TestGroupRPCAdminCRUD(t *testing.T) {
	h, svc, users := newRPCStack(t)
	ctx := t.Context()

	// Anonymous → the admin guard rejects.
	req := httptest.NewRequest(http.MethodPost, "/tango.identity.v1.UserGroupService/ListGroups", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	name := "grp_" + stamp()
	w = rpcPost(t, h, "CreateGroup", `{"name":"`+name+`","display_name":"Group `+name+`"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = rpcPost(t, h, "ListGroups", `{"page":1,"limit":10}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"metadata"`)

	// Validation failure → invalid_argument.
	w = rpcPost(t, h, "CreateGroup", `{"name":""}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Duplicate name → already_exists.
	w = rpcPost(t, h, "CreateGroup", `{"name":"`+name+`","display_name":"Again"}`)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Find the created group through the list to learn its ID.
	groups, _, err := svc.List(ctx, ListParams{Page: Page{Page: 1, Limit: 10}})
	require.NoError(t, err)
	require.Len(t, groups, 1)
	groupID := groups[0].ID.String()

	w = rpcPost(t, h, "GetGroup", `{"group_id":"`+groupID+`"}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), name)

	w = rpcPost(t, h, "UpdateGroup", `{"group_id":"`+groupID+`","display_name":"Renamed"}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Renamed")

	// Members: bind the provisioned user, list the projection,
	// replace with an empty set.
	memberID := userTypeID(t, users)
	w = rpcPost(t, h, "ReplaceGroupUsers", `{"group_id":"`+groupID+`","user_ids":["`+memberID+`"]}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = rpcPost(t, h, "ListGroupUsers", `{"group_id":"`+groupID+`","page":{"page":1,"limit":10}}`)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), memberID)

	w = rpcPost(t, h, "ReplaceGroupUsers", `{"group_id":"`+groupID+`","user_ids":[]}`)
	assert.Equal(t, http.StatusOK, w.Code)

	// Allowlist replacement with an unknown client → invalid_argument.
	w = rpcPost(t, h, "ReplaceAllowedOidcClients", `{"group_id":"`+groupID+`","client_ids":["client_a"]}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Unknown group → not_found.
	w = rpcPost(t, h, "GetGroup", `{"group_id":"ugrp_00000000000000000000000000"}`)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Delete.
	w = rpcPost(t, h, "DeleteGroup", `{"group_id":"`+groupID+`"}`)
	assert.Equal(t, http.StatusOK, w.Code)
}
