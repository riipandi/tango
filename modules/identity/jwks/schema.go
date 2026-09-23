package jwks

import (
	"time"
)

// TableJWKS is the table the published key set reads. It is owned by
// migration 00004; the rows are written by the OAuth provider work, not by
// this module.
const TableJWKS = "jwks"

// UseSignature is the `use_for` value of a key that signs and verifies.
// A published JWKS carries only these; an `enc` key is never handed to a
// client that asked how to check a signature.
const UseSignature = "sig"

// KeyUsageSignature is the JWK `use` value of a published key, the field a
// client reads to know the key verifies a signature rather than encrypts.
// It is the RFC 7517 spelling of UseSignature, which names a column.
const KeyUsageSignature = "sig"

// StoredKey is one row of TableJWKS, reduced to what publishing a key
// needs. The private key column is deliberately absent: this repository
// reads the public side only, so a private key cannot reach the endpoint
// through it. The key type is not carried either — it is inside the stored
// JWK, and a second copy could disagree with it.
type StoredKey struct {
	// KeyID is the `kid` a token names to select this key.
	KeyID string
	// Algorithm is the JWS algorithm the key is used with.
	Algorithm string
	// PublicKey is the stored public key. It is UTF-8 JWK JSON for a key
	// the provider generated, and an unprefixed or malformed value is a
	// row this module refuses rather than publishes.
	PublicKey []byte
	// ExpiresAt is when the key stops being valid. A nil value never
	// expires; an expired row is filtered out by the query.
	ExpiresAt *time.Time
}
