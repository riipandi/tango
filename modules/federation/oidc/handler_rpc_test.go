package oidc

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	federationv1 "github.com/riipandi/tango/codegen/proto/go/tango/federation/v1"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
)

func mustParseClientID(t *testing.T, raw string) OIDCClientID {
	t.Helper()
	id, err := OIDCParseClientID(raw)
	require.NoError(t, err)
	return id
}

func connectCode(t *testing.T, err error) *connect.Error {
	t.Helper()
	require.Error(t, err)
	var cerr *connect.Error
	require.True(t, errors.As(err, &cerr), "must decode as *connect.Error")
	return cerr
}

// TestRPCClientLifecycle covers the client surface through the
// generated contract: create (show-once secret), get, update, meta,
// secrets, allowed groups, and delete.
func TestRPCClientLifecycle(t *testing.T) {
	service, store, ds := testStack(t)
	h := &clientRPC{service: service}
	ctx := t.Context()

	created, err := h.CreateClient(ctx, connect.NewRequest(&federationv1.CreateOidcClientRequest{
		Name:         "RPC RP",
		CallbackUrls: []string{"https://rpc.example/callback"},
	}))
	require.NoError(t, err)
	assert.NotEmpty(t, created.Msg.GetClientSecret(), "plaintext secret is show-once")
	assert.Equal(t, "RPC RP", created.Msg.GetClient().GetName())
	assert.True(t, created.Msg.GetClient().GetHasSecret(), "confidential clients hold a usable secret")
	clientID := created.Msg.GetClient().GetId()

	got, err := h.GetClient(ctx, connect.NewRequest(&federationv1.GetOidcClientRequest{ClientId: clientID}))
	require.NoError(t, err)
	assert.Equal(t, "RPC RP", got.Msg.GetName())

	meta, err := h.GetClientMeta(ctx, connect.NewRequest(&federationv1.GetOidcClientRequest{ClientId: clientID}))
	require.NoError(t, err)
	assert.Equal(t, "RPC RP", meta.Msg.GetName())
	assert.False(t, meta.Msg.GetHasLogo())

	// Update is a full replacement (REST PUT semantics): callbacks
	// ride along.
	updated, err := h.UpdateClient(ctx, connect.NewRequest(&federationv1.UpdateOidcClientRequest{
		ClientId:     clientID,
		Name:         "RPC RP v2",
		CallbackUrls: []string{"https://rpc.example/callback"},
	}))
	require.NoError(t, err)
	assert.Equal(t, "RPC RP v2", updated.Msg.GetName())

	// Secrets: create is show-once, list never carries values.
	secret, err := h.CreateSecret(ctx, connect.NewRequest(&federationv1.CreateSecretRequest{ClientId: clientID}))
	require.NoError(t, err)
	assert.NotEmpty(t, secret.Msg.GetClientSecret())
	assert.NotEmpty(t, secret.Msg.GetSecret().GetId())

	listed, err := h.ListSecrets(ctx, connect.NewRequest(&federationv1.ListSecretsRequest{ClientId: clientID}))
	require.NoError(t, err)
	// The create-time secret plus the new entry.
	require.Len(t, listed.Msg.GetSecrets(), 2)

	// Allowed groups ride the atomic replacement; the group must
	// exist (FK).
	groups := usergroup.NewPostgresStore(ds)
	group, err := groups.Create(ctx, usergroup.CreateParams{
		Name: "grp_" + stamp(), DisplayName: "Group",
	})
	require.NoError(t, err)
	_, err = h.UpdateAllowedUserGroups(ctx, connect.NewRequest(&federationv1.UpdateAllowedUserGroupsRequest{
		ClientId: clientID, GroupIds: []string{group.ID.String()},
	}))
	require.NoError(t, err)
	fresh, err := store.GetClient(ctx, mustParseClientID(t, clientID))
	require.NoError(t, err)
	// The store returns UUID column values; the wire carries TypeIDs.
	assert.Equal(t, []string{group.ID.UUID()}, fresh.AllowedGroupIDs)

	_, err = h.DeleteSecret(ctx, connect.NewRequest(&federationv1.DeleteSecretRequest{
		ClientId: clientID, SecretId: secret.Msg.GetSecret().GetId(),
	}))
	require.NoError(t, err)

	_, err = h.DeleteClient(ctx, connect.NewRequest(&federationv1.DeleteOidcClientRequest{ClientId: clientID}))
	require.NoError(t, err)

	_, err = h.GetClient(ctx, connect.NewRequest(&federationv1.GetOidcClientRequest{ClientId: clientID}))
	assert.Equal(t, connect.CodeNotFound, connectCode(t, err).Code())
}

// TestRPCConsentScopes covers the consent surface: the self listings
// scope to the principal, the admin view sees everything, and
// revocation kills the grant.
func TestRPCConsentScopes(t *testing.T) {
	service, store, ds := testStack(t)
	h := &consentRPC{service: service}
	ctx := t.Context()

	users := user.NewPostgresStore(ds)
	u, err := users.Create(ctx, user.CreateParams{
		Username: "consent_rpc_" + stamp(), Email: "consent-rpc-" + stamp() + "@example.com"})
	require.NoError(t, err)

	client := clientFixture(ctx, t, store, "consent-"+stamp())
	require.NoError(t, store.UpsertAuthorizedClient(ctx, u.ID.UUID(), client.ID.String(), []string{"openid"}))

	selfCtx := middleware.WithPrincipal(ctx, principalFixture(u.ID.String()))

	mine, err := h.ListMyAuthorizedClients(selfCtx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	require.Len(t, mine.Msg.GetAuthorizedClients(), 1)
	assert.Equal(t, client.ID.String(), mine.Msg.GetAuthorizedClients()[0].GetClientId())

	mineAll, err := h.ListAllAuthorizedClients(selfCtx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	// The self context does not widen the admin listing: the guard
	// decides, the handler only resolves.
	assert.NotNil(t, mineAll.Msg)

	accessible, err := h.ListMyClients(selfCtx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.NotEmpty(t, accessible.Msg.GetClients())

	_, err = h.RevokeMyAuthorizedClient(selfCtx, connect.NewRequest(&federationv1.RevokeMyAuthorizedClientRequest{
		ClientId: client.ID.String(),
	}))
	require.NoError(t, err)

	mine, err = h.ListMyAuthorizedClients(selfCtx, connect.NewRequest(&commonv1.PageRequest{Page: 1, Limit: 20}))
	require.NoError(t, err)
	assert.Empty(t, mine.Msg.GetAuthorizedClients())
}

// TestRPCConsentUserListingRejectsUnknownUser pins the not-found
// contract of the admin consent listing: an id that is not a TypeID,
// or that names no user, must not reach the store query, where it
// would surface as a uuid cast failure and answer 500.
func TestRPCConsentUserListingRejectsUnknownUser(t *testing.T) {
	service, store, ds := testStack(t)
	h := &consentRPC{service: service}
	ctx := t.Context()

	users := user.NewPostgresStore(ds)
	u, err := users.Create(ctx, user.CreateParams{
		Username: "cu_" + stamp(), Email: "consent-unknown-" + stamp() + "@example.com"})
	require.NoError(t, err)

	client := clientFixture(ctx, t, store, "consent-unknown-"+stamp())
	require.NoError(t, store.UpsertAuthorizedClient(ctx, u.ID.UUID(), client.ID.String(), []string{"openid"}))

	// A well-formed TypeID that names no row.
	unknown := identity.NewID[user.UserID]()

	for name, id := range map[string]string{
		"unknown but well formed": unknown.String(),
		"not a typeid":            "not-an-id",
		"empty":                   "",
	} {
		t.Run(name, func(t *testing.T) {
			_, callErr := h.ListUserAuthorizedClients(ctx, connect.NewRequest(
				&federationv1.ListUserAuthorizedClientsRequest{UserId: id, Page: &commonv1.PageRequest{Page: 1, Limit: 20}}))
			assert.Equal(t, connect.CodeNotFound, connectCode(t, callErr).Code())
		})
	}

	// The known user still lists, so the guard did not narrow the happy path.
	got, err := h.ListUserAuthorizedClients(ctx, connect.NewRequest(
		&federationv1.ListUserAuthorizedClientsRequest{UserId: u.ID.String(), Page: &commonv1.PageRequest{Page: 1, Limit: 20}}))
	require.NoError(t, err)
	require.Len(t, got.Msg.GetAuthorizedClients(), 1)
}

// TestRPCRegistrationPrefixes pin the registration contracts the
// composition root relies on.
func TestRPCRegistrationPrefixes(t *testing.T) {
	service, _, _ := testStack(t)
	feature := New(service)

	clientPrefix, clientHandler := feature.RPCService()
	assert.Equal(t, "/tango.federation.v1.OidcClientService/", clientPrefix)
	assert.NotNil(t, clientHandler)

	consentPrefix, consentHandler := feature.ConsentRPCService()
	assert.Equal(t, "/tango.federation.v1.OidcConsentService/", consentPrefix)
	assert.NotNil(t, consentHandler)
}
