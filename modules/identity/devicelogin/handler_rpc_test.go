package devicelogin

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	identityv1 "github.com/riipandi/tango/codegen/proto/go/tango/identity/v1"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/user"
)

// featureStack builds the Feature over the service test stack.
func testStackForRPC(t *testing.T) (Feature, *Service, *user.PostgresStore) {
	t.Helper()
	service, sessions, users := testStack(t)
	_ = sessions
	return New(service), service, users.(*user.PostgresStore)
}

func featureRPC(t *testing.T, f Feature) *approvalRPC {
	t.Helper()
	prefix, handler := f.RPCService()
	_ = prefix
	_ = handler
	// The adapter is exercised directly; registration is pinned
	// separately.
	return &approvalRPC{service: f.service}
}

// mustRPCUser provisions an FK-valid user row.
func mustRPCUser(t *testing.T, users *user.PostgresStore, name string) user.User {
	t.Helper()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 10)
	u, err := users.Create(t.Context(), user.CreateParams{
		Username: name + "_" + stamp[len(stamp)-8:],
		Email:    name + "-" + stamp + "@example.com",
	})
	require.NoError(t, err)
	return u
}

// mustPendingRequest creates one pending device login request.
func mustPendingRequest(t *testing.T, service *Service, appURL string) (*Created, string) {
	t.Helper()
	created, token, err := service.Create(t.Context(), appURL, "10.0.0.1", "test-agent")
	require.NoError(t, err)
	return created, token
}

// connectCode decodes a Connect error; a non-Connect error fails.
func connectCode(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// TestRPCApprovalFlow drives the approval surface through the
// generated contract: inspect answers the pending device summary and
// the decision resolves the request.
func TestRPCApprovalFlow(t *testing.T) {
	feature, service, users := testStackForRPC(t)
	h := featureRPC(t, feature)
	ctx := t.Context()

	userRow := mustRPCUser(t, users, "approval")
	principal := middleware.Principal{SessionID: "sess_rpc", UserID: userRow.ID.String(), IsAdmin: true}
	authCtx := middleware.WithPrincipal(ctx, principal)

	created, deviceToken := mustPendingRequest(t, service, "https://sso.test")

	info, err := h.GetPendingRequest(authCtx, connect.NewRequest(&identityv1.GetPendingRequestRequest{
		UserCode: created.UserCode,
	}))
	require.NoError(t, err)
	assert.Equal(t, created.UserCode, info.Msg.GetUserCode())
	assert.NotEmpty(t, info.Msg.GetExpiresAt())
	_ = deviceToken

	_, err = h.DecideRequest(authCtx, connect.NewRequest(&identityv1.DecideRequestRequest{
		UserCode: created.UserCode,
		Approve:  true,
	}))
	require.NoError(t, err)

	// The decided request no longer inspects.
	_, err = h.GetPendingRequest(authCtx, connect.NewRequest(&identityv1.GetPendingRequestRequest{
		UserCode: created.UserCode,
	}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())
}

// TestRPCApprovalValidation pins the input contract: a blank code and
// an anonymous caller answer invalid_argument / unauthenticated.
func TestRPCApprovalValidation(t *testing.T) {
	feature, _, _ := testStackForRPC(t)
	h := featureRPC(t, feature)
	ctx := t.Context()

	_, err := h.GetPendingRequest(ctx, connect.NewRequest(&identityv1.GetPendingRequestRequest{UserCode: "   "}))
	assert.Equal(t, connect.CodeInvalidArgument, connectCode(t, err).Code())

	_, err = h.GetPendingRequest(ctx, connect.NewRequest(&identityv1.GetPendingRequestRequest{UserCode: "NOPE1234"}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())

	// Decision without a principal is unauthenticated.
	_, err = h.DecideRequest(ctx, connect.NewRequest(&identityv1.DecideRequestRequest{UserCode: "NOPE1234"}))
	assert.Equal(t, connect.CodeUnauthenticated, connectCode(t, err).Code())
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	feature, _, _ := testStackForRPC(t)
	prefix, handler := feature.RPCService()
	assert.Equal(t, "/tango.identity.v1.DeviceApprovalService/", prefix)
	assert.NotNil(t, handler)
}
