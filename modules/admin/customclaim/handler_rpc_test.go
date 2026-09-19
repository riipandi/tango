package customclaim

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

// rpcStack builds the Connect adapter over the shared test container.
func rpcStack(t *testing.T) (*claimRPC, *user.PostgresStore, *usergroup.PostgresStore) {
	t.Helper()
	store, users, groups := newTestStack(t)
	return &claimRPC{service: NewService(store, nil)}, users, groups
}

// TestRPCClaimLifecycle covers the claim surface through the
// generated contract: suggestions, user and group scopes, update,
// and delete — plus the duplicate conflict.
func TestRPCClaimLifecycle(t *testing.T) {
	h, users, groups := rpcStack(t)
	ctx := t.Context()
	stamp := strconvStamp()

	u, err := users.Create(ctx, user.CreateParams{
		Username: "rpc_u_" + stamp, Email: "rpc-u-" + stamp + "@example.com"})
	require.NoError(t, err)
	g, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "rpc_g_" + stamp, DisplayName: "RPC Group"})
	require.NoError(t, err)

	// Create on the user scope.
	created, err := h.CreateUserClaim(ctx, connect.NewRequest(&identityv1.CreateUserClaimRequest{
		UserId: u.ID.String(), Key: "role", Value: "vip",
	}))
	require.NoError(t, err)
	assert.Equal(t, "vip", created.Msg.GetValue())
	assert.Equal(t, u.ID.String(), created.Msg.GetUserId())

	// Duplicate (same owner + key) is a conflict.
	_, err = h.CreateUserClaim(ctx, connect.NewRequest(&identityv1.CreateUserClaimRequest{
		UserId: u.ID.String(), Key: "role", Value: "vip",
	}))
	cerr := connectErr(err)
	assert.Equal(t, connect.CodeAlreadyExists, cerr.Code())

	// Update the value.
	updated, err := h.UpdateUserClaim(ctx, connect.NewRequest(&identityv1.UpdateClaimRequest{
		UserId: u.ID.String(), ClaimId: created.Msg.GetId(), Value: "admin",
	}))
	require.NoError(t, err)
	assert.Equal(t, "admin", updated.Msg.GetValue())

	// List for the user.
	listed, err := h.ListUserClaims(ctx, connect.NewRequest(&identityv1.ListUserClaimsRequest{
		UserId: u.ID.String(),
	}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetClaims(), 1)

	// Group scope: same key, independent row.
	gCreated, err := h.CreateGroupClaim(ctx, connect.NewRequest(&identityv1.CreateGroupClaimRequest{
		UserGroupId: g.ID.String(), Key: "role", Value: "member",
	}))
	require.NoError(t, err)
	gListed, err := h.ListGroupClaims(ctx, connect.NewRequest(&identityv1.ListGroupClaimsRequest{
		UserGroupId: g.ID.String(),
	}))
	require.NoError(t, err)
	require.Len(t, gListed.Msg.GetClaims(), 1)
	assert.Equal(t, gCreated.Msg.GetId(), gListed.Msg.GetClaims()[0].GetId())

	// Suggestions list the keys in use.
	suggested, err := h.Suggest(ctx, connect.NewRequest(&identityv1.SuggestRequest{Query: ""}))
	require.NoError(t, err)
	assert.Contains(t, suggested.Msg.GetKeys(), "role")

	// Delete both scopes.
	_, err = h.DeleteUserClaim(ctx, connect.NewRequest(&identityv1.DeleteClaimRequest{
		UserId: u.ID.String(), ClaimId: created.Msg.GetId(),
	}))
	require.NoError(t, err)
	_, err = h.DeleteGroupClaim(ctx, connect.NewRequest(&identityv1.DeleteGroupClaimRequest{
		UserGroupId: g.ID.String(), ClaimId: gCreated.Msg.GetId(),
	}))
	require.NoError(t, err)

	empty, err := h.ListUserClaims(ctx, connect.NewRequest(&identityv1.ListUserClaimsRequest{
		UserId: u.ID.String(),
	}))
	require.NoError(t, err)
	assert.Empty(t, empty.Msg.GetClaims())
}

// TestRPCClaimErrors pins the error contract: malformed TypeIDs and
// validation failures map onto Connect codes.
func TestRPCClaimErrors(t *testing.T) {
	h, users, _ := rpcStack(t)
	ctx := t.Context()
	stamp := strconvStamp()

	u, err := users.Create(ctx, user.CreateParams{
		Username: "rpc_e_" + stamp, Email: "rpc-e-" + stamp + "@example.com"})
	require.NoError(t, err)

	_, err = h.ListUserClaims(ctx, connect.NewRequest(&identityv1.ListUserClaimsRequest{UserId: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectErr(err).Code())

	_, err = h.CreateUserClaim(ctx, connect.NewRequest(&identityv1.CreateUserClaimRequest{
		UserId: u.ID.String(), Key: "", Value: "v",
	}))
	assert.Equal(t, connect.CodeInvalidArgument, connectErr(err).Code())

	_, err = h.UpdateUserClaim(ctx, connect.NewRequest(&identityv1.UpdateClaimRequest{
		UserId: u.ID.String(), ClaimId: "nope", Value: "v",
	}))
	assert.Equal(t, connect.CodeNotFound, connectErr(err).Code())
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	h, _, _ := rpcStack(t)
	prefix, handler := h.service.RPCService()
	assert.Equal(t, "/tango.identity.v1.CustomClaimService/", prefix)
	assert.NotNil(t, handler)
}

func connectErr(err error) *connect.Error {
	var cerr *connect.Error
	if !errors.As(err, &cerr) {
		panic("error must decode as *connect.Error: " + err.Error())
	}
	return cerr
}

func strconvStamp() string {
	return strconv.FormatInt(time.Now().UnixNano(), 10)
}
