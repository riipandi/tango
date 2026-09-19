package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRateKeyMatchesDatabaseConstraint pins the key shape the
// rate_limits key check accepts (`^[a-z0-9_:]+$`). A policy name
// contains hyphens and an IPv6 address contains colons and dots; a
// key that fails the check makes the insert fail, which the limiter
// treats as a store error and fails open — silently disabling the
// budget.
func TestRateKeyMatchesDatabaseConstraint(t *testing.T) {
	cases := []struct {
		name   string
		remote string
		policy string
		want   string
	}{
		{"ipv4 with hyphenated policy", "192.0.2.1:1234", "forgot-password", "rl_forgot_password_192_0_2_1"},
		{"ipv6 with hyphenated policy", "[2001:db8::1]:443", "totp-recovery-codes", "rl_totp_recovery_codes_2001:db8::1"},
		{"simple policy", "127.0.0.1:8080", "signup", "rl_signup_127_0_0_1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			req.RemoteAddr = tc.remote
			key := rateKey(req, tc.policy)
			assert.Equal(t, tc.want, key)
			assert.Regexp(t, `^[a-z0-9_:]+$`, key, "the rate_limits key check must accept this key")
		})
	}
}

// TestEveryPolicyProducesAnAcceptedKey walks the real policy table:
// every policy name must survive key construction, because one
// rejected key silently disables that endpoint's budget.
func TestEveryPolicyProducesAnAcceptedKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.RemoteAddr = "192.0.2.1:1234"
	for name := range policies {
		key := rateKey(req, name)
		assert.Regexp(t, `^[a-z0-9_:]+$`, key, "policy %s produces a rejected key", name)
	}
}
