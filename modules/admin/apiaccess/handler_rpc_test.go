package apiaccess

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/pkg/testutils"
)

// rpcTestStack builds the Connect adapter over the shared test
// container.
func rpcTestStack(t *testing.T) (*apiRPC, datastore.Store) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	return &apiRPC{service: NewService(NewPostgresStore(ds), nil)}, ds
}

// seedClient inserts a relying-party client row and returns its key.
func seedClient(t *testing.T, ds datastore.Store, name string) string {
	t.Helper()
	client := oidc.NewID().String()
	_, err := ds.Exec(t.Context(),
		"INSERT INTO public.oidc_clients (id, name) VALUES ($1, $2)", client, name)
	require.NoError(t, err)
	return client
}

func connectErr(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// TestRPCAPILifecycle covers the registry surface through the
// generated contract: create, list with metadata, get, update,
// permissions replacement, CIMD access, and delete.
func TestRPCAPILifecycle(t *testing.T) {
	h, _ := rpcTestStack(t)
	ctx := t.Context()

	empty, err := h.ListApis(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.Empty(t, empty.Msg.GetApis())
	assert.NotNil(t, empty.Msg.GetMetadata())

	created, err := h.CreateApi(ctx, connect.NewRequest(&adminv1.CreateApiRequest{
		Name: "Core API", Resource: "https://api.test",
	}))
	require.NoError(t, err)
	assert.Equal(t, "Core API", created.Msg.GetName())
	assert.NotEmpty(t, created.Msg.GetId())

	listed, err := h.ListApis(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetApis(), 1)
	assert.Equal(t, int32(1), listed.Msg.GetMetadata().GetTotalItems())

	got, err := h.GetApi(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: created.Msg.GetId()}))
	require.NoError(t, err)
	assert.Equal(t, created.Msg.GetId(), got.Msg.GetId())

	updated, err := h.UpdateApi(ctx, connect.NewRequest(&adminv1.UpdateApiRequest{
		Id: created.Msg.GetId(), Name: proto.String("Core API v2"),
	}))
	require.NoError(t, err)
	assert.Equal(t, "Core API v2", updated.Msg.GetName())

	_, err = h.SetPermissions(ctx, connect.NewRequest(&adminv1.SetPermissionsRequest{
		Id: created.Msg.GetId(), PermissionIds: []string{"read", "write"},
	}))
	require.NoError(t, err)

	perm, err := h.GetApi(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: created.Msg.GetId()}))
	require.NoError(t, err)
	assert.Len(t, perm.Msg.GetPermissions(), 2)

	_, err = h.SetCimdAccess(ctx, connect.NewRequest(&adminv1.SetCimdAccessRequest{
		Id: created.Msg.GetId(), AllowedForCimdClients: true,
	}))
	require.NoError(t, err)

	// Duplicate resource is a conflict.
	_, err = h.CreateApi(ctx, connect.NewRequest(&adminv1.CreateApiRequest{
		Name: "Other", Resource: "https://api.test",
	}))
	cerr := connectErr(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, cerr.Code())

	_, err = h.DeleteApi(ctx, connect.NewRequest(&adminv1.DeleteApiRequest{Id: created.Msg.GetId()}))
	require.NoError(t, err)

	listed, err = h.ListApis(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.Empty(t, listed.Msg.GetApis())
}

// TestRPCGrantLifecycle covers the client-grant surface: grant,
// revoke, client listings, and the per-client API views.
func TestRPCGrantLifecycle(t *testing.T) {
	h, ds := rpcTestStack(t)
	ctx := t.Context()

	created, err := h.CreateApi(ctx, connect.NewRequest(&adminv1.CreateApiRequest{
		Name: "Grant API", Resource: "https://grant.test",
	}))
	require.NoError(t, err)
	apiID := created.Msg.GetId()
	_, err = h.SetPermissions(ctx, connect.NewRequest(&adminv1.SetPermissionsRequest{
		Id: apiID, PermissionIds: []string{"read"},
	}))
	require.NoError(t, err)

	// Unknown client is rejected.
	_, err = h.GrantClient(ctx, connect.NewRequest(&adminv1.GrantClientRequest{
		ApiId: apiID, ClientId: "oidc_client_unknown",
	}))
	cerr := connectErr(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, cerr.Code())

	// Seed a real client directly through the store's fixture path.
	clientID := seedClient(t, ds, "grant-client-"+strconv.FormatInt(time.Now().UnixNano(), 10))

	_, err = h.GrantClient(ctx, connect.NewRequest(&adminv1.GrantClientRequest{
		ApiId: apiID, ClientId: clientID, ClientAccess: true,
	}))
	require.NoError(t, err)

	withAccess, err := h.ListClients(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: apiID}))
	require.NoError(t, err)
	require.Len(t, withAccess.Msg.GetClients(), 1)
	assert.Equal(t, clientID, withAccess.Msg.GetClients()[0].GetId())

	assignable, err := h.ListAssignableClients(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: apiID}))
	require.NoError(t, err)
	assert.Empty(t, assignable.Msg.GetClients())

	grants, err := h.ListApisForClient(ctx, connect.NewRequest(&adminv1.ClientApisRequest{ClientId: clientID}))
	require.NoError(t, err)
	require.Len(t, grants.Msg.GetGrants(), 1)
	assert.True(t, grants.Msg.GetGrants()[0].GetClientAccess())
	assert.Equal(t, apiID, grants.Msg.GetGrants()[0].GetApi().GetId())

	assignableAPIs, err := h.ListAssignableApisForClient(ctx, connect.NewRequest(&adminv1.ClientApisRequest{ClientId: clientID}))
	require.NoError(t, err)
	assert.Empty(t, assignableAPIs.Msg.GetApis())

	_, err = h.RevokeClient(ctx, connect.NewRequest(&adminv1.RevokeClientRequest{
		ApiId: apiID, ClientId: clientID,
	}))
	require.NoError(t, err)

	withAccess, err = h.ListClients(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: apiID}))
	require.NoError(t, err)
	assert.Empty(t, withAccess.Msg.GetClients())
}

// TestRPCAPIErrors pins the error contract: malformed TypeIDs and
// validation failures map onto Connect codes.
func TestRPCAPIErrors(t *testing.T) {
	h, _ := rpcTestStack(t)
	ctx := t.Context()

	_, err := h.GetApi(ctx, connect.NewRequest(&adminv1.GetApiRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectErr(t, err).Code())

	_, err = h.CreateApi(ctx, connect.NewRequest(&adminv1.CreateApiRequest{Name: "", Resource: "https://x.test"}))
	assert.Equal(t, connect.CodeInvalidArgument, connectErr(t, err).Code())
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	h, _ := rpcTestStack(t)
	prefix, handler := h.service.RPCService()
	assert.Equal(t, "/tango.admin.v1.ApiService/", prefix)
	assert.NotNil(t, handler)
}
