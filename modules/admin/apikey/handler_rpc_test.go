package apikey

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
)

// rpcTestStack builds the connect adapter over the shared test
// container with a real user row; the caller context carries the
// principal directly (bearer middleware has its own tests).
func rpcTestStack(t *testing.T) (context.Context, *apiKeyRPC) {
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
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(ctx, user.CreateParams{
		Username:    "keyrpc" + stamp[len(stamp)-8:],
		Email:       "keyrpc" + stamp + "@test.local",
		DisplayName: "Key RPC",
	})
	require.NoError(t, err)

	principal := kernel.Principal{SessionID: "sess_rpc", UserID: u.ID.String(), IsAdmin: true}
	svcCtx := middleware.WithPrincipal(ctx, principal)
	return svcCtx, &apiKeyRPC{service: NewService(NewPostgresStore(ds), nil)}
}

// futureRFC3339 returns an expiry just past the validation floor.
func futureRFC3339() string {
	return time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
}

func connectErr(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// TestRPCKeyLifecycle covers every method through the generated
// contract: list (empty and populated), create with a show-once
// token, renew, and delete.
func TestRPCKeyLifecycle(t *testing.T) {
	ctx, h := rpcTestStack(t)

	empty, err := h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.Empty(t, empty.Msg.GetApiKeys())
	assert.NotNil(t, empty.Msg.GetMetadata())

	created, err := h.Create(ctx, connect.NewRequest(&adminv1.CreateApiKeyRequest{
		Name:        "RPC Key",
		Description: &[]string{"from rpc"}[0],
		ExpiresAt:   futureRFC3339(),
	}))
	require.NoError(t, err)
	assert.NotEmpty(t, created.Msg.GetToken(), "plaintext token is show-once")
	assert.Equal(t, "RPC Key", created.Msg.GetApiKey().GetName())

	listed, err := h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetApiKeys(), 1)
	meta := listed.Msg.GetMetadata()
	assert.Equal(t, int32(1), meta.GetTotalItems())

	renewed, err := h.Renew(ctx, connect.NewRequest(&adminv1.RenewApiKeyRequest{
		Id:        created.Msg.GetApiKey().GetId(),
		ExpiresAt: futureRFC3339(),
	}))
	// Renewal of an unexpired key is a conflict (REST 409).
	cerr := connectErr(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, cerr.Code())
	assert.Nil(t, renewed)

	deleted, err := h.Delete(ctx, connect.NewRequest(&adminv1.DeleteApiKeyRequest{
		Id: created.Msg.GetApiKey().GetId(),
	}))
	require.NoError(t, err)
	assert.NotNil(t, deleted.Msg)

	// The revoked key disappears from the listing.
	listed, err = h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.Empty(t, listed.Msg.GetApiKeys())
}

// TestRPCKeyErrors pins the error contract: anonymous callers,
// malformed TypeIDs, bad expiry timestamps, and validation failures
// map onto Connect codes with REST-equivalent meanings.
func TestRPCKeyErrors(t *testing.T) {
	ctx, h := rpcTestStack(t)

	anonCtx := t.Context()
	_, err := h.List(anonCtx, connect.NewRequest(&commonv1.PageRequest{}))
	cerr := connectErr(t, err)
	assert.Equal(t, connect.CodeUnauthenticated, cerr.Code())

	_, err = h.Renew(ctx, connect.NewRequest(&adminv1.RenewApiKeyRequest{
		Id:        "not-a-typeid",
		ExpiresAt: futureRFC3339(),
	}))
	cerr = connectErr(t, err)
	assert.Equal(t, connect.CodeNotFound, cerr.Code())

	_, err = h.Create(ctx, connect.NewRequest(&adminv1.CreateApiKeyRequest{
		Name:      "RPC Key",
		ExpiresAt: "yesterday",
	}))
	cerr = connectErr(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, cerr.Code())

	_, err = h.Create(ctx, connect.NewRequest(&adminv1.CreateApiKeyRequest{
		Name:      "ab",
		ExpiresAt: futureRFC3339(),
	}))
	cerr = connectErr(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, cerr.Code())
	assert.Contains(t, cerr.Message(), "validation failed")
}

// TestRPCPagination pins the shared pagination block contract.
func TestRPCPagination(t *testing.T) {
	ctx, h := rpcTestStack(t)
	for i := range 3 {
		_, err := h.Create(ctx, connect.NewRequest(&adminv1.CreateApiKeyRequest{
			Name:      "rpc-key-" + strconv.Itoa(i),
			ExpiresAt: futureRFC3339(),
		}))
		require.NoError(t, err)
	}

	listed, err := h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 2}))
	require.NoError(t, err)
	meta := listed.Msg.GetMetadata()
	require.NotNil(t, meta)
	assert.Equal(t, int32(3), meta.GetTotalItems())
	assert.Equal(t, int32(2), meta.GetTotalPages())
	assert.Equal(t, int32(1), meta.GetFirstItemIndex())
	assert.Equal(t, int32(2), meta.GetLastItemIndex())
	assert.Len(t, listed.Msg.GetApiKeys(), 2)

	// The all-marker shape (REST "all" convention) stays unpaged.
	all, err := h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: -1, Limit: -1}))
	require.NoError(t, err)
	assert.Nil(t, all.Msg.GetMetadata())
	assert.Len(t, all.Msg.GetApiKeys(), 3)
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	_, h := rpcTestStack(t)
	prefix, handler := h.service.RPCService()
	assert.Equal(t, "/tango.admin.v1.ApiKeyService/", prefix)
	assert.NotNil(t, handler)
}
