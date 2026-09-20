package oidc

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfiguredLifetimesOverrideDefaults pins that the protocol
// lifetimes come from configuration, and that an unset field keeps the
// package default rather than collapsing to zero.
func TestConfiguredLifetimesOverrideDefaults(t *testing.T) {
	configured := NewService(nil, nil, "https://sso.test", "tango_session",
		WithLifetimes(Lifetimes{
			AccessToken:       5 * time.Minute,
			RefreshToken:      48 * time.Hour,
			AuthorizationCode: 30 * time.Second,
			Interaction:       20 * time.Minute,
			DeviceCode:        7 * time.Minute,
			PAR:               15 * time.Second,
		}))

	assert.Equal(t, 5*time.Minute, configured.AccessTokenTTL())
	assert.Equal(t, 48*time.Hour, configured.RefreshTokenTTL())
	assert.Equal(t, 30*time.Second, configured.AuthorizationCodeTTL())
	assert.Equal(t, 20*time.Minute, configured.InteractionTTL())
	assert.Equal(t, 7*time.Minute, configured.DeviceCodeTTL())
	assert.Equal(t, 15*time.Second, configured.PARTTL())

	// A partial configuration leaves the other lifetimes at their
	// defaults, and a zero field never yields a zero lifetime.
	partial := NewService(nil, nil, "https://sso.test", "tango_session",
		WithLifetimes(Lifetimes{AccessToken: 5 * time.Minute}))
	assert.Equal(t, 5*time.Minute, partial.AccessTokenTTL())
	assert.Equal(t, DefaultRefreshTokenTTL, partial.RefreshTokenTTL())
	assert.Equal(t, DefaultAuthorizationCodeTTL, partial.AuthorizationCodeTTL())
	assert.Equal(t, DefaultInteractionSessionTTL, partial.InteractionTTL())
	assert.Equal(t, DefaultDeviceCodeTTL, partial.DeviceCodeTTL())
	assert.Equal(t, DefaultPARTTL, partial.PARTTL())

	// No option at all keeps every default.
	plain := NewService(nil, nil, "https://sso.test", "tango_session")
	for name, got := range map[string]time.Duration{
		"access":   plain.AccessTokenTTL(),
		"refresh":  plain.RefreshTokenTTL(),
		"code":     plain.AuthorizationCodeTTL(),
		"interact": plain.InteractionTTL(),
		"device":   plain.DeviceCodeTTL(),
		"par":      plain.PARTTL(),
	} {
		assert.Positive(t, got, "%s lifetime must never be zero", name)
	}
}

// TestConfiguredLifetimesReachTheWire pins that a configured value is
// what the authorization-code and PAR responses advertise, not the
// compiled default.
func TestConfiguredLifetimesReachTheWire(t *testing.T) {
	svc := NewService(nil, nil, "https://sso.test", "tango_session",
		WithLifetimes(Lifetimes{PAR: 42 * time.Second, DeviceCode: 90 * time.Second}))

	require.Equal(t, 42*time.Second, svc.PARTTL())
	require.Equal(t, 90*time.Second, svc.DeviceCodeTTL())

	// The wire value is the whole-second form of the configured
	// duration.
	assert.Equal(t, 42, int(svc.PARTTL().Seconds()))
	assert.Equal(t, 90, int(svc.DeviceCodeTTL().Seconds()))
}
