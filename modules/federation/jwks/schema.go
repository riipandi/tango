// Package jwks owns signing key material: generation, encrypted
// storage, rotation with overlap, and the JWKS key provider consumed
// by token issuance and the discovery endpoints.
package jwks

import (
	"time"

	"go.jetify.com/typeid"
)

// Typed IDs for the jwks tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	jwkPrefix struct{}

	JWKID = typeid.TypeID[jwkPrefix]
)

func (jwkPrefix) Prefix() string { return "jwk" }

// Table is the fully-qualified key storage table.
const jwksTable = "public.jwks"

// NewID mints a typed key ID; its string form doubles as the JWK kid.
func NewID() JWKID { return typeid.Must(typeid.New[JWKID]()) }

// Storage and protocol constants. The provider signs with the newest
// active key; retired keys stay published until their overlap window
// closes so verifiers can validate tokens minted before the swap.
const (
	Table = "jwks"

	// UseSignature marks signing keys; UseEncryption marks key
	// exchange material (reserved).
	UseSignature  = "sig"
	UseEncryption = "enc"

	// RS256 is the default signing algorithm (RSA 2048).
	RS256 = "RS256"
	// ES256 is the alternative (ECDSA P-256).
	ES256 = "ES256"

	// RotationOverlap keeps a retired key published this long after
	// it stops signing, so outstanding tokens stay verifiable.
	RotationOverlap = 24 * time.Hour

	// CacheTTL bounds provider-side key caching; kept in sync with
	// the discovery Cache-Control max-age.
	CacheTTL = 5 * time.Minute
)

// KeyType names the JWK kty value stored alongside the algorithm.
const (
	KeyTypeRSA = "RSA"
	KeyTypeEC  = "EC"
)
