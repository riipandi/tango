package appconfig

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
)

// TestRPCConfigLifecycle covers the settings surface through the
// generated contract: the anonymous bootstrap view lists only public
// keys, the admin view lists everything with sensitive values
// redacted, update persists a key, and unknown keys are ignored.
func TestRPCConfigLifecycle(t *testing.T) {
	_, module := newStoreStack(t)
	h := &configRPC{module: module}

	// The public view carries only public keys.
	pub, err := h.Get(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.NotEmpty(t, pub.Msg.GetVariables())
	for _, v := range pub.Msg.GetVariables() {
		assert.True(t, v.GetIsPublic(), v.GetKey()+" must be public in the bootstrap view")
	}

	// The admin view lists every catalog key.
	all, err := h.GetAll(t.Context(), connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	assert.Greater(t, len(all.Msg.GetVariables()), len(pub.Msg.GetVariables()))

	// Update persists a known key and ignores unknown ones.
	updated, err := h.Update(t.Context(), connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "app_name", Type: adminv1.ConfigVariable_TYPE_STRING, Value: "rpc-tango"},
			{Key: "not_a_real_key", Value: "ignored"},
		},
	}))
	require.NoError(t, err)
	var appName string
	for _, v := range updated.Msg.GetVariables() {
		if v.GetKey() == "app_name" {
			appName = v.GetValue()
		}
	}
	assert.Equal(t, "rpc-tango", appName)

	// Validation rejects a bad value for a known key.
	_, err = h.Update(t.Context(), connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "disable_animations", Type: adminv1.ConfigVariable_TYPE_BOOL, Value: "maybe"},
		},
	}))
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, connectErrAs(err, &cerr))
	assert.Equal(t, connect.CodeInvalidArgument, cerr.Code())
}

// TestRPCRegistrationPrefix pins the registration contract the
// composition root relies on.
func TestRPCRegistrationPrefix(t *testing.T) {
	_, module := newStoreStack(t)
	prefix, handler := module.RPCService(nil)
	assert.Equal(t, "/tango.admin.v1.ApplicationConfigurationService/", prefix)
	assert.NotNil(t, handler)
}

// TestRPCConfigBootstrapIsAnonymous pins the pre-sign-in contract the
// SPA reads: Get answers without a credential, and the admin read
// stays protected. The guard is the only thing that could demand a
// principal here, so the request goes through the mounted handler.
func TestRPCConfigBootstrapIsAnonymous(t *testing.T) {
	_, module := newStoreStack(t)
	prefix, handler := module.RPCService(nil)

	call := func(procedure string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, prefix+procedure, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Connect-Protocol-Version", "1")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}

	bootstrap := call("Get")
	require.Equal(t, http.StatusOK, bootstrap.Code, bootstrap.Body.String())
	assert.Contains(t, bootstrap.Body.String(), "app_name")

	admin := call("GetAll")
	require.Equal(t, http.StatusUnauthorized, admin.Code, admin.Body.String())
	assert.Contains(t, admin.Body.String(), `"code":"unauthenticated"`)
}

func connectErrAs(err error, target **connect.Error) bool {
	return errors.As(err, target)
}
