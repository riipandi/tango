package jwtutils

import (
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"github.com/lestrrat-go/jwx/v3/jwt"
)

// Signer mints compact, signed JWTs whose private claims are the
// typed set T. Build one per key; the With* methods return modified
// copies, so a configured signer is safe to share.
type Signer[T any] struct {
	key       jwk.Key
	algorithm jwa.SignatureAlgorithm
	issuer    string
	audience  []string
	ttl       time.Duration
}

// NewSigner builds a signer over key with the given signature
// algorithm.
func NewSigner[T any](key jwk.Key, algorithm jwa.SignatureAlgorithm) (*Signer[T], error) {
	if key == nil {
		return nil, ErrMissingKey
	}
	return &Signer[T]{key: key, algorithm: algorithm}, nil
}

// WithIssuer sets the default iss claim, applied when Standard
// leaves it empty.
func (s *Signer[T]) WithIssuer(issuer string) *Signer[T] {
	clone := *s
	clone.issuer = issuer
	return &clone
}

// WithAudience sets the default aud claim, applied when Standard
// leaves it empty.
func (s *Signer[T]) WithAudience(audience ...string) *Signer[T] {
	clone := *s
	clone.audience = audience
	return &clone
}

// WithTTL sets the default token lifetime, applied when Standard
// leaves ExpiresAt empty.
func (s *Signer[T]) WithTTL(ttl time.Duration) *Signer[T] {
	clone := *s
	clone.ttl = ttl
	return &clone
}

// Sign produces the compact JWT: registered claims from std (zero
// values fall back to the signer defaults) plus the private claims
// flattened from the typed set.
func (s *Signer[T]) Sign(claims T, std Standard) (string, error) {
	tok := jwt.New()

	issuedAt := std.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now()
	}
	if std.Issuer != "" || s.issuer != "" {
		issuer := std.Issuer
		if issuer == "" {
			issuer = s.issuer
		}
		if err := tok.Set(jwt.IssuerKey, issuer); err != nil {
			return "", err
		}
	}
	if std.Subject != "" {
		if err := tok.Set(jwt.SubjectKey, std.Subject); err != nil {
			return "", err
		}
	}
	audience := std.Audience
	if len(audience) == 0 {
		audience = s.audience
	}
	if len(audience) > 0 {
		if err := tok.Set(jwt.AudienceKey, audience); err != nil {
			return "", err
		}
	}
	if err := tok.Set(jwt.IssuedAtKey, issuedAt); err != nil {
		return "", err
	}
	if !std.NotBefore.IsZero() {
		if err := tok.Set(jwt.NotBeforeKey, std.NotBefore); err != nil {
			return "", err
		}
	}
	if !std.ExpiresAt.IsZero() || s.ttl > 0 {
		expiresAt := std.ExpiresAt
		if expiresAt.IsZero() {
			expiresAt = issuedAt.Add(s.ttl)
		}
		if err := tok.Set(jwt.ExpirationKey, expiresAt); err != nil {
			return "", err
		}
	}
	if std.JWTID != "" {
		if err := tok.Set(jwt.JwtIDKey, std.JWTID); err != nil {
			return "", err
		}
	}

	flat, err := jsonv2.Marshal(claims)
	if err != nil {
		return "", err
	}
	var private map[string]any
	if unmarshalErr := jsonv2.Unmarshal(flat, &private); unmarshalErr != nil {
		return "", unmarshalErr
	}
	for name, value := range private {
		if setErr := tok.Set(name, value); setErr != nil {
			return "", setErr
		}
	}

	signed, err := jwt.Sign(tok, jwt.WithKey(s.algorithm, s.key))
	if err != nil {
		return "", err
	}
	return string(signed), nil
}
