package federation_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/federation/jwks"
	"github.com/riipandi/tango/modules/federation/oidc"
	"github.com/riipandi/tango/modules/federation/scimsync"
)

func TestEveryPrefixIsLowercaseSnakeCase(t *testing.T) {
	for _, id := range []interface{ Prefix() string }{
		typeid.Must(typeid.New[jwks.JWKID]()),
		typeid.Must(typeid.New[oidc.OIDCClientID]()),
		typeid.Must(typeid.New[oidc.AuthorizationCodeID]()),
		typeid.Must(typeid.New[oidc.OIDCRefreshTokenID]()),
		typeid.Must(typeid.New[oidc.DeviceCodeID]()),
		typeid.Must(typeid.New[oidc.OAuth2SessionID]()),
		typeid.Must(typeid.New[oidc.OAuth2JTIID]()),
		typeid.Must(typeid.New[oidc.InteractionSessionID]()),
		typeid.Must(typeid.New[scimsync.SCIMServiceProviderID]()),
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
