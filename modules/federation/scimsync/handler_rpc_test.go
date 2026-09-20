package scimsync

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
)

// TestRPCProviderLifecycle covers the provider surface through the
// generated contract: create (token echoed once), update (absent
// fields keep the current value), sync, and delete.
func TestRPCProviderLifecycle(t *testing.T) {
	store, _ := newStore(t)
	service := NewService(store, nil, httpPoster{client: &http.Client{Timeout: 5 * time.Second}}, nil)
	h := &providerRPC{service: service}
	ctx := t.Context()
	clientID := clientFixtureID

	created, err := h.Upsert(ctx, connect.NewRequest(&federationv1.UpsertScimProviderRequest{
		Endpoint:     "https://scim.test",
		Token:        "provider-token",
		OidcClientId: &clientID,
	}))
	require.NoError(t, err)
	assert.Equal(t, "https://scim.test", created.Msg.GetEndpoint())
	assert.Equal(t, "provider-token", created.Msg.GetToken(), "token echoes on write only")
	assert.Equal(t, clientID, created.Msg.GetOidcClientId())
	providerID := created.Msg.GetId()

	// Unknown client is rejected.
	_, err = h.Upsert(ctx, connect.NewRequest(&federationv1.UpsertScimProviderRequest{
		Endpoint: "https://scim.test", Token: "t",
	}))
	cerr := connectError(t, err)
	assert.Equal(t, connect.CodeInvalidArgument, cerr.Code())

	// Update keeps the token when absent, replaces the endpoint.
	newEndpoint := "https://scim2.test"
	updated, err := h.Update(ctx, connect.NewRequest(&federationv1.UpdateScimProviderRequest{
		Id: providerID, Endpoint: &newEndpoint,
	}))
	require.NoError(t, err)
	assert.Equal(t, newEndpoint, updated.Msg.GetEndpoint())
	assert.Equal(t, "provider-token", updated.Msg.GetToken())

	// Duplicate binding is a conflict.
	_, err = h.Upsert(ctx, connect.NewRequest(&federationv1.UpsertScimProviderRequest{
		Endpoint: "https://scim.test", Token: "t", OidcClientId: &clientID,
	}))
	cerr = connectError(t, err)
	assert.Equal(t, connect.CodeAlreadyExists, cerr.Code())

	// Sync on a provider whose receiver is unreachable surfaces the
	// fixed internal error.
	_, err = h.Sync(ctx, connect.NewRequest(&federationv1.SyncScimProviderRequest{Id: providerID}))
	cerr = connectError(t, err)
	assert.Equal(t, connect.CodeInternal, cerr.Code())

	_, err = h.Delete(ctx, connect.NewRequest(&federationv1.DeleteScimProviderRequest{Id: providerID}))
	require.NoError(t, err)

	_, err = h.Sync(ctx, connect.NewRequest(&federationv1.SyncScimProviderRequest{Id: providerID}))
	assert.Equal(t, connect.CodeNotFound, connectError(t, err).Code())
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	store, _ := newStore(t)
	feature := New(NewService(store, nil, nil, nil))
	prefix, handler := feature.RPCService()
	assert.Equal(t, "/tango.federation.v1.ScimProviderService/", prefix)
	assert.NotNil(t, handler)
}

func connectError(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}
