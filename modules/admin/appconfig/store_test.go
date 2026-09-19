package appconfig

import (
	"errors"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/riipandi/tango/database"
	adminv1 "github.com/riipandi/tango/gen/proto/go/tango/admin/v1"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/testutils"
)

// newStoreStack boots a throwaway Postgres, applies migrations, and
// returns the real store (raw stored values) and data store wired
// into the cipher-sealed module.
func newStoreStack(t *testing.T) (Store, datastore.Store, *Module) {
	t.Helper()
	ctx := t.Context()

	pg := testutils.StartPostgres(ctx, t)
	if _, err := database.MigrateUp(ctx, pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}

	ds, err := datastore.New(ctx, datastore.Options{DSN: pg.DSN})
	require.NoError(t, err)
	t.Cleanup(func() { ds.Close() })

	cipher, err := crypto.NewCipher(make([]byte, 32))
	require.NoError(t, err)

	store := NewPostgresStore(ds)
	return store, ds, func() *Module {
		return New(nil).WithStore(store).WithCipher(cipher)
	}()
}

// TestConfigCRUD covers the settings surface through the generated
// contract: defaults fold with overrides, validation rejects bad
// values, unknown keys are ignored.
func TestConfigCRUD(t *testing.T) {
	_, _, module := newStoreStack(t)
	h := &configRPC{module: module}
	ctx := t.Context()

	// Public view (no guard): defaults only, public keys.
	pub, err := h.Get(ctx, connect.NewRequest(&emptypb.Empty{}))
	require.NoError(t, err)
	require.NotEmpty(t, pub.Msg.GetVariables())
	for _, v := range pub.Msg.GetVariables() {
		assert.False(t, strings.HasPrefix(v.GetKey(), "smtp"), "env-only keys never leak: %s", v.GetKey())
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
	assert.False(t, names["session_duration"], "private key present but flagged")

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

	// Bad enum, bad int, bad JSON shape → invalid_argument.
	for _, tc := range []struct{ key, value string }{
		{"allow_user_signups", "sometimes"},
		{"session_duration", "soon"},
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

// TestEnvDefaultsAndSensitiveRedaction folds env defaults over the
// catalog and DB overrides on top; sensitive values never reach the
// wire, while MergedValues still serves the real secret to wired
// consumers.
func TestEnvDefaultsAndSensitiveRedaction(t *testing.T) {
	store, _, module := newStoreStack(t)
	module = module.WithEnvDefaults(map[string]string{
		"smtp_host":     "relay.example",
		"smtp_password": "env-secret",
	})
	h := &configRPC{module: module}
	ctx := t.Context()

	fetch := func() map[string]string {
		resp, err := h.GetAll(ctx, connect.NewRequest(&emptypb.Empty{}))
		require.NoError(t, err)
		values := map[string]string{}
		for _, v := range resp.Msg.GetVariables() {
			values[v.GetKey()] = v.GetValue()
		}
		return values
	}

	// Env layer sits above the catalog defaults; the env password is
	// still redacted in the admin view.
	values := fetch()
	assert.Equal(t, "relay.example", values["smtp_host"])
	assert.Equal(t, "", values["smtp_password"], "sensitive values redact even from env")

	// Real values reach consumers through MergedValues.
	merged, err := module.MergedValues(ctx)
	require.NoError(t, err)
	assert.Equal(t, "relay.example", merged["smtp_host"])
	assert.Equal(t, "env-secret", merged["smtp_password"])

	// DB overrides top the env layer; the stored secret stays hidden.
	_, err = h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "smtp_host", Type: adminv1.ConfigVariable_TYPE_STRING, Value: "db-relay.example"},
			{Key: "smtp_password", Type: adminv1.ConfigVariable_TYPE_STRING, Value: "db-secret"},
		},
	}))
	require.NoError(t, err)

	values = fetch()
	assert.Equal(t, "db-relay.example", values["smtp_host"])
	assert.Equal(t, "", values["smtp_password"])

	merged, err = module.MergedValues(ctx)
	require.NoError(t, err)
	assert.Equal(t, "db-secret", merged["smtp_password"])
	assert.Equal(t, "db-relay.example", merged["smtp_host"])

	// The stored override is sealed: the raw row carries the enc:
	// marker, never the plaintext.
	stored, err := store.List(ctx)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(stored["smtp_password"], "enc:"),
		"stored sensitive value must carry the enc: marker, got %q", stored["smtp_password"])
}

func TestClearSensitiveValue(t *testing.T) {
	store, _, module := newStoreStack(t)
	h := &configRPC{module: module}
	ctx := t.Context()

	// Set a real secret first.
	_, err := h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "smtp_password", Type: adminv1.ConfigVariable_TYPE_STRING, Value: "db-secret"},
		},
	}))
	require.NoError(t, err)

	// Clearing it answers without error and removes the row.
	_, err = h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "smtp_password", Type: adminv1.ConfigVariable_TYPE_STRING, Value: ""},
		},
	}))
	require.NoError(t, err)

	stored, err := store.List(ctx)
	require.NoError(t, err)
	assert.NotContains(t, stored, "smtp_password", "cleared secret must not leave a row")

	// Clearing again is a no-op, not an error.
	_, err = h.Update(ctx, connect.NewRequest(&adminv1.UpdateConfigVariablesRequest{
		Variables: []*adminv1.ConfigVariable{
			{Key: "smtp_password", Type: adminv1.ConfigVariable_TYPE_STRING, Value: ""},
		},
	}))
	require.NoError(t, err)
}
