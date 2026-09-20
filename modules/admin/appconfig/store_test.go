package appconfig

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/testutils"
)

// newStoreStack boots a throwaway Postgres, applies migrations, and
// returns the real store (raw stored values) wired into the module.
func newStoreStack(t *testing.T) (Store, *Module) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	store := NewPostgresStore(ds)
	return store, New(nil).WithStore(store)
}

// TestConfigCRUD covers the settings surface through the generated
// contract: defaults fold with overrides, validation rejects bad
// values, unknown keys are ignored.
func TestConfigCRUD(t *testing.T) {
	_, module := newStoreStack(t)
	h := &configRPC{module: module}
	ctx := t.Context()

	// Public view (no guard): defaults only, public keys.
	pub, err := h.Get(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.NotEmpty(t, pub.Msg.GetVariables())
	for _, v := range pub.Msg.GetVariables() {
		assert.True(t, v.GetIsPublic(), v.GetKey()+" must be public in the bootstrap view")
	}

	// Admin /all includes private keys with the is_public flag.
	all, err := h.GetAll(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.NotEmpty(t, all.Msg.GetVariables())

	names := map[string]bool{}
	for _, v := range all.Msg.GetVariables() {
		names[v.GetKey()] = v.GetIsPublic()
	}
	assert.True(t, names["app_name"], "public key visible")
	assert.False(t, names["cimd_url_allowlist"], "private key present but flagged")

	// Update persists keys, ignores unknown ones, and echoes the full view.
	updated, err := h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "app_name", Value: "Tango"},
			{Key: "allow_user_signups", Value: "open"},
			{Key: "no_such_key", Value: "x"},
		},
	}))
	require.NoError(t, err)
	values := map[string]string{}
	for _, v := range updated.Msg.GetVariables() {
		values[v.GetKey()] = v.GetValue()
	}
	assert.Equal(t, "Tango", values["app_name"])
	assert.Equal(t, "open", values["allow_user_signups"])
	assert.NotContains(t, values, "no_such_key")

	// The override survives a re-read of the public view.
	pub, err = h.Get(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	found := false
	for _, v := range pub.Msg.GetVariables() {
		if v.GetKey() == "app_name" {
			found = true
			assert.Equal(t, "Tango", v.GetValue())
		}
	}
	assert.True(t, found, "app_name in public view")

	// Bad enum and bad JSON shape → invalid_argument.
	for _, tc := range []struct{ key, value string }{
		{"allow_user_signups", "sometimes"},
		{"webauthn_user_verification", "optional"},
		{"cimd_url_allowlist", "not-json"},
		{"cimd_url_allowlist", "[\"ok\", 4]"},
	} {
		_, err := h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
			Variables: []*adminv1.ConfigVariable{{Key: tc.key, Value: tc.value}},
		}))
		require.Error(t, err, tc.key)
		var cerr *connect.Error
		require.True(t, errors.As(err, &cerr), tc.key)
		assert.Equal(t, connect.CodeInvalidArgument, cerr.Code(), tc.key)
	}
}

// TestSMTPIsEnvOnly pins the single-source rule for the relay: no
// SMTP key is admin-editable, so an update naming one is ignored like
// any unknown key, and the merged values never carry one.
func TestSMTPIsEnvOnly(t *testing.T) {
	store, module := newStoreStack(t)
	h := &configRPC{module: module}
	ctx := t.Context()

	_, err := h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "smtp_host", Value: "attacker.example"},
			{Key: "smtp_password", Value: "attacker-secret"},
		},
	}))
	require.NoError(t, err)

	all, err := h.GetAll(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	for _, v := range all.Msg.GetVariables() {
		assert.False(t, strings.HasPrefix(v.GetKey(), "smtp"), "SMTP key is not editable: %s", v.GetKey())
	}

	merged, err := module.MergedValues(ctx)
	require.NoError(t, err)
	for key := range merged {
		assert.False(t, strings.HasPrefix(key, "smtp"), "SMTP key never enters merged values: %s", key)
	}

	// Nothing was written either: the unknown keys never reach the store.
	stored, err := store.List(ctx)
	require.NoError(t, err)
	assert.NotContains(t, stored, "smtp_host")
	assert.NotContains(t, stored, "smtp_password")
}
