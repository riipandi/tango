package jwtutils

import (
	"errors"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jws"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Errors returned by the signer and verifier.
var (
	ErrMissingKey    = errors.New("jwtutils: signing key is required")
	ErrMissingKeySet = errors.New("jwtutils: verification key set is empty")
)

// Verifier checks the signature and registered-claim constraints of a
// JWT, then decodes its private claims into the typed set T. Like the
// signer, the With* methods return modified copies.
type Verifier[T any] struct {
	key       jwk.Key
	algorithm jwa.SignatureAlgorithm
	keySet    jwk.Set
	issuer    string
	audience  string
}

// NewVerifier builds a verifier over a single key with the given
// signature algorithm. The key may be nil when verification will use
// a key set instead (WithKeySet).
func NewVerifier[T any](key jwk.Key, algorithm jwa.SignatureAlgorithm) (*Verifier[T], error) {
	return &Verifier[T]{key: key, algorithm: algorithm}, nil
}

// WithKeySet switches verification to a JWKS key set: the token's kid
// selects the key (kid is required).
func (v *Verifier[T]) WithKeySet(set jwk.Set) *Verifier[T] {
	clone := *v
	clone.keySet = set
	return &clone
}

// WithIssuer requires the iss claim to equal issuer when set.
func (v *Verifier[T]) WithIssuer(issuer string) *Verifier[T] {
	clone := *v
	clone.issuer = issuer
	return &clone
}

// WithAudience requires the aud claim to contain audience when set.
func (v *Verifier[T]) WithAudience(audience string) *Verifier[T] {
	clone := *v
	clone.audience = audience
	return &clone
}

// Verify checks the signature and constraints, then decodes the
// typed private claims.
func (v *Verifier[T]) Verify(encoded string) (Verified[T], error) {
	opts := []jwt.ParseOption{}
	switch {
	case v.keySet != nil:
		if v.keySet.Len() == 0 {
			return Verified[T]{}, ErrMissingKeySet
		}
		opts = append(opts, jwt.WithKeySet(v.keySet, jws.WithRequireKid(true)))
	case v.key != nil:
		opts = append(opts, jwt.WithKey(v.algorithm, v.key))
	default:
		return Verified[T]{}, ErrMissingKey
	}
	if v.issuer != "" {
		opts = append(opts, jwt.WithIssuer(v.issuer))
	}
	if v.audience != "" {
		opts = append(opts, jwt.WithAudience(v.audience))
	}

	tok, err := jwt.Parse([]byte(encoded), opts...)
	if err != nil {
		return Verified[T]{}, err
	}

	private, err := decodePrivate[T](tok)
	if err != nil {
		return Verified[T]{}, err
	}

	out := Verified[T]{Private: private}
	if issuer, ok := tok.Issuer(); ok {
		out.Issuer = issuer
	}
	if subject, ok := tok.Subject(); ok {
		out.Subject = subject
	}
	if audience, ok := tok.Audience(); ok {
		out.Audience = audience
	}
	if expiresAt, ok := tok.Expiration(); ok {
		out.ExpiresAt = expiresAt
	}
	if issuedAt, ok := tok.IssuedAt(); ok {
		out.IssuedAt = issuedAt
	}
	if notBefore, ok := tok.NotBefore(); ok {
		out.NotBefore = notBefore
	}
	if jwtID, ok := tok.JwtID(); ok {
		out.JWTID = jwtID
	}
	return out, nil
}
