package jwtutils

import (
	"context"
	"fmt"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
)

// AccessClaims are the private claims an access token carries. The subject is
// the account's ID; every other field is duplicated here so a verifier reads
// the token without a round trip. The type is shared because more than one
// consumer decodes it: the feature that signs and every seam that verifies.
type AccessClaims struct {
	Email       string `json:"email"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
	SessionID   string `json:"sid"`
}

// SigningKeySource supplies the material the process signs and verifies its
// own access tokens with, across the dual stack: a symmetric algorithm signs
// with the HMAC secret, anything else with the configured key pair.
//
// jwks.Service satisfies it. The cached KeyProvider does not — an HMAC secret
// is not in the published set, so a verifier resolves through the key service
// itself and a rotation is picked up on the next call.
type SigningKeySource interface {
	KeyProvider
	HMACKey(ctx context.Context) (jwk.Key, error)
	SigningAlgorithm() (jwa.SignatureAlgorithm, error)
}

// AccessVerifier verifies the access tokens this process signs. The key and
// algorithm resolve on every call, and the verifier is built with that exact
// pair, so the accepted algorithm is pinned by construction — a token signed
// under another algorithm fails before any key material is tried.
type AccessVerifier struct {
	keys   SigningKeySource
	issuer string
}

// NewAccessVerifier builds the verifier over the key service and the issuer
// the deployment signs with.
func NewAccessVerifier(keys SigningKeySource, issuer string) *AccessVerifier {
	return &AccessVerifier{keys: keys, issuer: issuer}
}

// Verify checks the token and decodes its typed private claims.
func (v *AccessVerifier) Verify(ctx context.Context, token string) (Verified[AccessClaims], error) {
	algorithm, err := v.keys.SigningAlgorithm()
	if err != nil {
		return Verified[AccessClaims]{}, fmt.Errorf("jwtutils: signing algorithm: %w", err)
	}

	var key jwk.Key
	if algorithm.IsSymmetric() {
		key, err = v.keys.HMACKey(ctx)
	} else {
		key, err = v.keys.SignKey(ctx)
	}
	if err != nil {
		return Verified[AccessClaims]{}, fmt.Errorf("jwtutils: signing key: %w", err)
	}

	verifier, err := NewVerifier[AccessClaims](key, algorithm)
	if err != nil {
		return Verified[AccessClaims]{}, fmt.Errorf("jwtutils: verifier: %w", err)
	}
	return verifier.WithIssuer(v.issuer).Verify(token)
}
