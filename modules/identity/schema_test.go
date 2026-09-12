package identity

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIDGeneratesPrefixedUUIDv7(t *testing.T) {
	id := NewID[UserID]()

	assert.Equal(t, "user", id.Prefix())

	// The suffix decodes to a UUIDv7 (RFC 9562): version nibble is 7.
	parsed, err := uuid.Parse(id.UUID())
	require.NoError(t, err)
	assert.Equal(t, uuid.Version(7), parsed.Version())
}

func TestParseIDRejectsWrongPrefix(t *testing.T) {
	raw := NewID[UserID]().String()

	_, err := ParseID[SessionID](raw)

	require.Error(t, err)
}

func TestParseIDRoundTrip(t *testing.T) {
	id := NewID[UserGroupID]()

	parsed, err := ParseID[UserGroupID](id.String())

	require.NoError(t, err)
	assert.Equal(t, id, parsed)
}

func TestEveryPrefixIsLowercaseSnakeCase(t *testing.T) {
	for _, id := range []interface{ Prefix() string }{
		NewID[UserID](), NewID[UserGroupID](), NewID[UserPhoneID](),
		NewID[SessionID](), NewID[AuthTokenID](), NewID[SignupTokenID](),
		NewID[RefreshTokenID](), NewID[InvitationID](), NewID[MFAKeyID](),
		NewID[WebauthnCredentialID](), NewID[WebauthnSessionID](),
		NewID[APIKeyID](), NewID[OAuthConnectionID](),
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
