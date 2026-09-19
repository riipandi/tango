package webhook

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	webhookv1 "github.com/riipandi/tango/codegen/proto/go/tango/webhook/v1"
)

// rpcStack builds the Connect adapter over the service test stack.
func rpcStack(t *testing.T) (*hookRPC, *testStack) {
	t.Helper()
	stack := newTestStack(t, okSender())
	return &hookRPC{service: stack.Service}, stack
}

// connectCode decodes a Connect error; a non-Connect error fails.
func connectCode(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// mustCreateViaRPC registers an endpoint and returns the wire row.
func mustCreateViaRPC(t *testing.T, h *hookRPC, name, endpoint string) *webhookv1.Webhook {
	t.Helper()
	created, err := h.Create(t.Context(), connect.NewRequest(&webhookv1.CreateWebhookRequest{
		Name:     name,
		Endpoint: endpoint,
	}))
	require.NoError(t, err)
	return created.Msg
}

// TestRPCWebhookLifecycle covers the endpoint surface through the
// generated contract: create (show-once secret), list with metadata,
// get, update, and delete.
func TestRPCWebhookLifecycle(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	created, err := h.Create(ctx, connect.NewRequest(&webhookv1.CreateWebhookRequest{
		Name:       "rpc-lifecycle",
		Endpoint:   "https://example.test/hook",
		EventTypes: []string{"user.created"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "rpc-lifecycle", created.Msg.GetName())
	require.NotNil(t, created.Msg.GetSecret(), "create must return the plaintext secret once")
	hookID := created.Msg.GetId()

	listed, err := h.List(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 10}))
	require.NoError(t, err)
	require.Len(t, listed.Msg.GetWebhooks(), 1)
	assert.Empty(t, listed.Msg.GetWebhooks()[0].GetSecret(), "listings must not carry the secret")
	require.NotNil(t, listed.Msg.GetMetadata())
	assert.Equal(t, int32(1), listed.Msg.GetMetadata().GetTotalItems())

	fetched, err := h.Get(ctx, connect.NewRequest(&webhookv1.GetWebhookRequest{Id: hookID}))
	require.NoError(t, err)
	assert.Empty(t, fetched.Msg.GetSecret())

	newName := "rpc-renamed"
	enabled := false
	updated, err := h.Update(ctx, connect.NewRequest(&webhookv1.UpdateWebhookRequest{
		Id: hookID, Name: &newName, Enabled: &enabled,
	}))
	require.NoError(t, err)
	assert.Equal(t, newName, updated.Msg.GetName())
	assert.False(t, updated.Msg.GetEnabled())

	_, err = h.Delete(ctx, connect.NewRequest(&webhookv1.DeleteWebhookRequest{Id: hookID}))
	require.NoError(t, err)

	_, err = h.Get(ctx, connect.NewRequest(&webhookv1.GetWebhookRequest{Id: hookID}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())
}

// TestRPCWebhookValidation pins the create contract: short names,
// missing endpoints, and bad schemes answer invalid_argument.
func TestRPCWebhookValidation(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	for _, tc := range []struct {
		name string
		req  *webhookv1.CreateWebhookRequest
	}{
		{"short name", &webhookv1.CreateWebhookRequest{Name: "ab", Endpoint: "https://example.test"}},
		{"missing endpoint", &webhookv1.CreateWebhookRequest{Name: "valid-name"}},
		{"bad scheme", &webhookv1.CreateWebhookRequest{Name: "valid-name", Endpoint: "ftp://example.test"}},
		{"relative endpoint", &webhookv1.CreateWebhookRequest{Name: "valid-name", Endpoint: "/hook"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.Create(ctx, connect.NewRequest(tc.req))
			assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err).Code())
		})
	}
}

// TestRPCWebhookDuplicateNameConflicts pins the unique-name rule.
func TestRPCWebhookDuplicateNameConflicts(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	body := &webhookv1.CreateWebhookRequest{Name: "rpc-dup", Endpoint: "https://example.test/hook"}
	_, err := h.Create(ctx, connect.NewRequest(body))
	require.NoError(t, err)
	_, err = h.Create(ctx, connect.NewRequest(body))
	assert.Equal(t, connect.CodeAlreadyExists, connectCode(t, err).Code())
}

// TestRPCWebhookUnknownIDs pins the not-found boundary: malformed and
// unknown IDs never distinguish existence.
func TestRPCWebhookUnknownIDs(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	_, err := h.Get(ctx, connect.NewRequest(&webhookv1.GetWebhookRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())

	_, err = h.Update(ctx, connect.NewRequest(&webhookv1.UpdateWebhookRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())

	_, err = h.Delete(ctx, connect.NewRequest(&webhookv1.DeleteWebhookRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())

	_, err = h.RotateSecret(ctx, connect.NewRequest(&webhookv1.GetWebhookRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())

	_, err = h.Test(ctx, connect.NewRequest(&webhookv1.TestWebhookRequest{Id: "not-a-typeid"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())
}

// TestRPCRotateSecret covers rotation: a new show-once secret.
func TestRPCRotateSecret(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	created := mustCreateViaRPC(t, h, "rpc-rotate", "https://example.test/hook")
	firstSecret := created.GetSecret()

	rotated, err := h.RotateSecret(ctx, connect.NewRequest(&webhookv1.GetWebhookRequest{Id: created.GetId()}))
	require.NoError(t, err)
	assert.NotEmpty(t, rotated.Msg.GetSecret())
	assert.NotEqual(t, firstSecret, rotated.Msg.GetSecret(), "rotation must issue a new secret")
}

// TestRPCTestDelivery covers the synthetic delivery: a queued log row
// addressable through the delivery listings.
func TestRPCTestDelivery(t *testing.T) {
	h, _ := rpcStack(t)
	ctx := t.Context()

	created := mustCreateViaRPC(t, h, "rpc-test", "https://example.test/hook")

	sent, err := h.Test(ctx, connect.NewRequest(&webhookv1.TestWebhookRequest{Id: created.GetId()}))
	require.NoError(t, err)
	assert.NotEmpty(t, sent.Msg.GetDeliveryId())

	scoped, err := h.ListDeliveries(ctx, connect.NewRequest(&webhookv1.ListDeliveriesRequest{
		WebhookId: created.GetId(),
		Page:      &commonv1.PageRequest{Page: 1, Limit: 10},
	}))
	require.NoError(t, err)
	require.Len(t, scoped.Msg.GetDeliveries(), 1)
	assert.Equal(t, created.GetId(), scoped.Msg.GetDeliveries()[0].GetWebhookId())

	global, err := h.ListAllDeliveries(ctx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 10}))
	require.NoError(t, err)
	assert.Len(t, global.Msg.GetDeliveries(), 1)
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	stack := newTestStack(t, okSender())
	module := New(stack.Service)
	prefix, handler := module.RPCService()
	assert.Equal(t, "/tango.webhook.v1.WebhookService/", prefix)
	assert.NotNil(t, handler)
}
