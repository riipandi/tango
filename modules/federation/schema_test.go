package federation

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jetify.com/typeid"
)

func TestEveryPrefixIsLowercaseSnakeCase(t *testing.T) {
	for _, id := range []interface{ Prefix() string }{
		typeid.Must(typeid.New[JWKID]()),
		typeid.Must(typeid.New[OIDCClientID]()),
		typeid.Must(typeid.New[CustomClaimID]()),
		typeid.Must(typeid.New[AuthorizationCodeID]()),
		typeid.Must(typeid.New[OIDCRefreshTokenID]()),
		typeid.Must(typeid.New[DeviceCodeID]()),
		typeid.Must(typeid.New[OAuth2SessionID]()),
		typeid.Must(typeid.New[OAuth2JTIID]()),
		typeid.Must(typeid.New[InteractionSessionID]()),
		typeid.Must(typeid.New[SCIMServiceProviderID]()),
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
