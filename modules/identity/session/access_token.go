package session

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// The internal access token is a short-lived RS256 JWT the auth
// worker holds in memory and sends as `Authorization: Bearer` on RPC
// calls. Issuer and audience are pinned away from the OIDC values so
// an internal token can never be exchanged for a relying-party token
// or vice versa.
const (
	// DefaultAccessTokenTTL bounds the bearer token's usefulness
	// after exfiltration when no lifetime is configured; refresh
	// stays cookie-only.
	DefaultAccessTokenTTL = 10 * time.Minute
	// AccessTokenIssuer and AccessTokenAudience pin the internal
	// token space; OIDC tokens carry the public issuer URL and a
	// client id instead.
	AccessTokenIssuer   = "tango:internal"
	AccessTokenAudience = "tango:rpc"
	// AccessTokenCookieName mirrors the access token in an HttpOnly
	// cookie scoped to the auth bridge paths, so the worker can
	// bootstrap without a refresh rotation right after sign-in.
	AccessTokenCookieName = "tango_access"
	// AccessTokenPath scopes the access cookie to the bridge.
	AccessTokenPath = "/api/auth"
)

// AccessClaims are the private claims of an access token; the
// standard subject carries the user TypeID.
type AccessClaims struct {
	// SessionID is the owning session TypeID — the link that keeps
	// RPC revocation exact even inside the token TTL.
	SessionID string `json:"sid"`
	Admin     bool   `json:"admin"`
}

// AccessTokenSigner mints and verifies internal access tokens with
// the shared signing key provider.
type AccessTokenSigner struct {
	provider jwtutils.KeyProvider
	ttl      time.Duration
}

// AccessTokenSignerOption configures the signer.
type AccessTokenSignerOption func(*AccessTokenSigner)

// WithAccessTokenTTL sets the bearer token lifetime; a non-positive
// value keeps the default.
func WithAccessTokenTTL(ttl time.Duration) AccessTokenSignerOption {
	return func(a *AccessTokenSigner) {
		if ttl > 0 {
			a.ttl = ttl
		}
	}
}

// NewAccessTokenSigner builds the access-token service on top of a
// cached key provider.
func NewAccessTokenSigner(provider jwtutils.KeyProvider, opts ...AccessTokenSignerOption) *AccessTokenSigner {
	a := &AccessTokenSigner{provider: provider, ttl: DefaultAccessTokenTTL}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Issue signs a fresh access token for the principal and returns it
// with its expiry.
func (a *AccessTokenSigner) Issue(ctx context.Context, p kernel.Principal) (string, time.Time, error) {
	key, err := a.provider.SignKey(ctx)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session: access sign key: %w", err)
	}
	baseSigner, err := jwtutils.NewSigner[AccessClaims](key, jwa.RS256())
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session: access signer: %w", err)
	}
	signer := baseSigner.
		WithIssuer(AccessTokenIssuer).
		WithAudience(AccessTokenAudience).
		WithTTL(a.ttl)

	now := time.Now()
	signed, err := signer.Sign(AccessClaims{SessionID: p.SessionID, Admin: p.IsAdmin}, jwtutils.Standard{
		Subject:   p.UserID,
		IssuedAt:  now,
		ExpiresAt: now.Add(a.ttl),
	})
	if err != nil {
		return "", time.Time{}, fmt.Errorf("session: access sign: %w", err)
	}
	return signed, now.Add(a.ttl), nil
}

// Verify checks signature, issuer, audience, and expiry.
func (a *AccessTokenSigner) Verify(ctx context.Context, token string) (jwtutils.Verified[AccessClaims], error) {
	key, err := a.provider.SignKey(ctx)
	if err != nil {
		return jwtutils.Verified[AccessClaims]{}, fmt.Errorf("session: access verify key: %w", err)
	}
	baseVerifier, err := jwtutils.NewVerifier[AccessClaims](key, jwa.RS256())
	if err != nil {
		return jwtutils.Verified[AccessClaims]{}, fmt.Errorf("session: access verifier: %w", err)
	}
	verifier := baseVerifier.
		WithIssuer(AccessTokenIssuer).
		WithAudience(AccessTokenAudience).
		WithRequiredClaims("sub", "exp")
	return verifier.Verify(token)
}
