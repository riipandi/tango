// Package jwks will own signing key material: generation, rotation,
// and the public JWKS document.
package jwks

import "go.jetify.com/typeid"

// Typed IDs for the jwks tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	jwkPrefix struct{}

	JWKID = typeid.TypeID[jwkPrefix]
)

func (jwkPrefix) Prefix() string { return "jwk" }
