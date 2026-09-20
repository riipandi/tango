package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLifetimesAreConfigurable pins that every authentication
// lifetime resolves from the environment layer, in seconds.
func TestLifetimesAreConfigurable(t *testing.T) {
	path := writeEnvFile(t, `
AUTH_ACCESS_TOKEN_EXPIRY=120
AUTH_SESSION_LIFETIME=86400
AUTH_SESSION_SHORT_LIFETIME=3600
OIDC_ACCESS_TOKEN_EXPIRY=300
OIDC_REFRESH_TOKEN_EXPIRY=604800
OIDC_AUTHORIZATION_CODE_EXPIRY=30
OIDC_INTERACTION_EXPIRY=600
OIDC_DEVICE_CODE_EXPIRY=180
OIDC_PAR_EXPIRY=45
`)
	cfg, err := Load(LoadOptions{EnvFile: path})
	require.NoError(t, err)

	assert.Equal(t, 120, cfg.Auth.AccessTokenExpiry)
	assert.Equal(t, 86400, cfg.Auth.SessionLifetime)
	assert.Equal(t, 3600, cfg.Auth.SessionShortLifetime)
	assert.Equal(t, 300, cfg.OIDC.AccessTokenExpiry)
	assert.Equal(t, 604800, cfg.OIDC.RefreshTokenExpiry)
	assert.Equal(t, 30, cfg.OIDC.AuthorizationCodeExpiry)
	assert.Equal(t, 600, cfg.OIDC.InteractionExpiry)
	assert.Equal(t, 180, cfg.OIDC.DeviceCodeExpiry)
	assert.Equal(t, 45, cfg.OIDC.PARExpiry)
}

// TestLifetimeValidation pins the guards that keep a typo from
// turning into an already-expired token or an inverted remember-me
// choice.
func TestLifetimeValidation(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want string
	}{
		{"zero session lifetime", "AUTH_SESSION_LIFETIME=0", "auth.session_lifetime"},
		{"negative access token", "AUTH_ACCESS_TOKEN_EXPIRY=-1", "auth.access_token_expiry"},
		{"session lifetime beyond a year", "AUTH_SESSION_LIFETIME=31536001", "auth.session_lifetime"},
		{"zero oidc refresh", "OIDC_REFRESH_TOKEN_EXPIRY=0", "oidc.refresh_token_expiry"},
		{"zero par", "OIDC_PAR_EXPIRY=0", "oidc.par_expiry"},
		{
			"short session exceeds the remembered one",
			"AUTH_SESSION_LIFETIME=3600\nAUTH_SESSION_SHORT_LIFETIME=7200",
			"auth.session_short_lifetime",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEnvFile(t, tc.env)
			_, err := Load(LoadOptions{EnvFile: path})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
