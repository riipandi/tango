package identity_test

import (
	"testing"

	"uuid"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/apikey"
	"github.com/riipandi/tango/modules/identity/multifactor"
	"github.com/riipandi/tango/modules/identity/oauthconnections"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/signup"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/modules/identity/webauthn"
)

func TestNewIDGeneratesPrefixedUUIDv7(t *testing.T) {
	id := identity.NewID[user.UserID]()

	assert.Equal(t, "user", id.Prefix())

	// The suffix decodes to a UUIDv7 (RFC 9562): version nibble is 7.
	parsed, err := uuid.Parse(id.UUID())
	require.NoError(t, err)
	assert.Equal(t, byte(7), parsed[6]>>4)
}

func TestParseIDRejectsWrongPrefix(t *testing.T) {
	raw := identity.NewID[user.UserID]().String()

	_, err := identity.ParseID[session.SessionID](raw)

	require.Error(t, err)
}

func TestParseIDRoundTrip(t *testing.T) {
	id := identity.NewID[usergroup.UserGroupID]()

	parsed, err := identity.ParseID[usergroup.UserGroupID](id.String())

	require.NoError(t, err)
	assert.Equal(t, id, parsed)
}

func TestEveryPrefixIsLowercaseSnakeCase(t *testing.T) {
	for _, id := range []interface{ Prefix() string }{
		identity.NewID[user.UserID](), identity.NewID[user.UserPhoneID](),
		identity.NewID[usergroup.UserGroupID](),
		identity.NewID[session.SessionID](), identity.NewID[session.AuthTokenID](),
		identity.NewID[session.RefreshTokenID](),
		identity.NewID[signup.SignupTokenID](), identity.NewID[signup.InvitationID](),
		identity.NewID[webauthn.WebauthnCredentialID](),
		identity.NewID[webauthn.WebauthnSessionID](),
		identity.NewID[multifactor.MFAKeyID](),
		identity.NewID[apikey.APIKeyID](),
		identity.NewID[oauthconnections.OAuthConnectionID](),
	} {
		prefix := id.Prefix()
		assert.NotEmpty(t, prefix)
		assert.LessOrEqual(t, len(prefix), 63, prefix)
		assert.NotContains(t, prefix, " ", prefix)
		for _, r := range prefix {
			assert.True(t, (r >= 'a' && r <= 'z') || r == '_', prefix)
		}
	}
}
