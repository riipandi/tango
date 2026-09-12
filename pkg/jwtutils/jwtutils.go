// Package jwtutils wraps lestrrat-go/jwx with typed claims: sign and
// verify JWTs whose custom claims are a plain struct — the struct's
// JSON tags name the private claims, while registered claims (iss,
// sub, aud, exp, nbf, iat, jti) stay explicit and validated.
package jwtutils

import (
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/lestrrat-go/jwx/v3/jwt"
)

// registeredClaims are the claim names handled by Standard; they are
// never part of the typed private claim set.
var registeredClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true,
	"exp": true, "nbf": true, "iat": true, "jti": true,
}

// Standard carries the registered claims managed by the Signer and
// surfaced by the Verifier.
type Standard struct {
	Issuer    string
	Subject   string
	Audience  []string
	ExpiresAt time.Time
	IssuedAt  time.Time
	NotBefore time.Time
	JWTID     string
}

// Verified couples the decoded registered claims with the typed
// private claims of a checked token.
type Verified[T any] struct {
	Standard
	Private T
}

// PrivateClaimDecoder is the optional customization point for typed
// claim sets: implement it on *T to take over reconstruction from the
// token's private claims (field aliases, defaults, derived fields).
// The default is a JSON round-trip honoring the struct's tags.
type PrivateClaimDecoder interface {
	DecodePrivateClaims(params map[string]any) error
}

// decodePrivate reconstructs the typed private claim set from the
// token's non-registered claims.
func decodePrivate[T any](tok jwt.Token) (T, error) {
	params := make(map[string]any, len(tok.Keys()))
	for _, name := range tok.Keys() {
		if registeredClaims[name] {
			continue
		}
		var value any
		if err := tok.Get(name, &value); err != nil {
			var zero T
			return zero, err
		}
		params[name] = value
	}

	var claims T
	if dec, ok := any(&claims).(PrivateClaimDecoder); ok {
		return claims, dec.DecodePrivateClaims(params)
	}

	raw, err := jsonv2.Marshal(params)
	if err != nil {
		return claims, err
	}
	return claims, jsonv2.Unmarshal(raw, &claims)
}
