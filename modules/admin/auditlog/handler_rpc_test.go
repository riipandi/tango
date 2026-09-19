package auditlog

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/database"
	adminv1 "github.com/riipandi/tango/gen/proto/go/tango/admin/v1"
	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/testutils"
)

// rpcStack builds the Connect adapter over the shared test container
// with an FK-valid user row for audit attribution.
func rpcStack(t *testing.T) *logRPC {
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
		Username: "auditrpc_" + stamp[len(stamp)-8:],
		Email:    "audit-rpc-" + stamp + "@test.local",
	})
	require.NoError(t, err)
	fixedPrincipal.UserID = u.ID.UUID()
	return &logRPC{module: New(NewPostgresStore(ds))}
}

// connectErrCode decodes a Connect error and returns its code; a
// non-Connect error fails the test.
func connectErrCode(t *testing.T, err error) connect.Code {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr.Code()
}

// TestRPCListScopesAndAdmin covers the read surface: the self listing
// scopes to the principal, the admin listing sees everything, and
// filter facets answer per kind.
func TestRPCListScopesAndAdmin(t *testing.T) {
	h := rpcStack(t)
	ctx := t.Context()

	require.NoError(t, h.module.Record(ctx, &Entry{Event: "user.created", UserID: strPtr(fixedPrincipal.UserID)}))
	require.NoError(t, h.module.Record(ctx, &Entry{Event: "user.signed_out"}))

	selfCtx := middleware.WithPrincipal(ctx, fixedPrincipal)

	self, err := h.List(selfCtx, connect.NewRequest(&adminv1.ListAuditLogsRequest{
		Page: &commonv1.PageRequest{Page: 1, Limit: 20},
	}))
	require.NoError(t, err)
	require.NotEmpty(t, self.Msg.GetLogs())
	for _, entry := range self.Msg.GetLogs() {
		assert.Equal(t, "user.created", entry.GetEvent())
	}

	all, err := h.ListAll(selfCtx, connect.NewRequest(&adminv1.ListAuditLogsRequest{
		Page: &commonv1.PageRequest{Page: 1, Limit: 20},
	}))
	require.NoError(t, err)
	require.Len(t, all.Msg.GetLogs(), 2)
	assert.NotNil(t, all.Msg.GetMetadata())

	users, err := h.FilterOptions(selfCtx, connect.NewRequest(&adminv1.FilterOptionsRequest{Kind: "users"}))
	require.NoError(t, err)
	assert.NotEmpty(t, users.Msg.GetValues())

	names, err := h.FilterOptions(selfCtx, connect.NewRequest(&adminv1.FilterOptionsRequest{Kind: "client-names"}))
	require.NoError(t, err)
	assert.NotNil(t, names.Msg)

	_, err = h.FilterOptions(selfCtx, connect.NewRequest(&adminv1.FilterOptionsRequest{Kind: "bogus"}))
	assert.Equal(t, connect.CodeInvalidArgument, connectErrCode(t, err))
}

// TestRPCListValidation pins the filter contract: a garbage user_id
// and non-RFC3339 timestamps answer invalid_argument.
func TestRPCListValidation(t *testing.T) {
	h := rpcStack(t)
	ctx := middleware.WithPrincipal(t.Context(), fixedPrincipal)

	_, err := h.ListAll(ctx, connect.NewRequest(&adminv1.ListAuditLogsRequest{UserId: strPtr("not-a-uuid")}))
	assert.Equal(t, connect.CodeInvalidArgument, connectErrCode(t, err))

	_, err = h.ListAll(ctx, connect.NewRequest(&adminv1.ListAuditLogsRequest{From: strPtr("yesterday")}))
	assert.Equal(t, connect.CodeInvalidArgument, connectErrCode(t, err))
}

// TestRPCAnonymousList pins that the self listing demands a
// principal.
func TestRPCAnonymousList(t *testing.T) {
	h := rpcStack(t)
	_, err := h.List(t.Context(), connect.NewRequest(&adminv1.ListAuditLogsRequest{}))
	assert.Equal(t, connect.CodeUnauthenticated, connectErrCode(t, err))
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	h := rpcStack(t)
	prefix, handler := h.module.RPCService(nil)
	assert.Equal(t, "/tango.admin.v1.AuditLogService/", prefix)
	assert.NotNil(t, handler)
}

func strPtr(s string) *string { return &s }
