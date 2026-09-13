package jwks

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/riipandi/tango/pkg/crypto"
)

// Service owns the signing-key lifecycle: first-boot generation,
// rotation with publication overlap, and the jwtutils.KeyProvider
// implementation consumed by token issuance and discovery.
type Service struct {
	store     Store
	cipher    *crypto.Cipher
	algorithm string
}

// NewService builds the key service. algorithm is RS256 or ES256.
func NewService(store Store, cipher *crypto.Cipher, algorithm string) *Service {
	if algorithm == "" {
		algorithm = RS256
	}
	return &Service{store: store, cipher: cipher, algorithm: algorithm}
}

// Start guarantees a signing key exists; called in registry order
// so token issuance never races a cold database.
func (s *Service) Start(ctx context.Context) error {
	if _, err := s.store.ActiveSigningKey(ctx); err == nil {
		return nil
	} else if err != ErrNoActiveKey {
		return fmt.Errorf("jwks: check signing key: %w", err)
	}
	return s.EnsureSigningKey(ctx)
}

// Stop implements kernel.Startable; no background resources.
func (s *Service) Stop(context.Context) error { return nil }

// Name implements federation.Feature.
func (s *Service) Name() string { return "jwks" }

// EnsureSigningKey creates the first signing key when none exists.
func (s *Service) EnsureSigningKey(ctx context.Context) error {
	key, err := GenerateKey(s.algorithm)
	if err != nil {
		return err
	}
	return s.insertKey(ctx, key)
}

// Rotate retires the current signing key (kept published for the
// overlap window) and activates a fresh one.
func (s *Service) Rotate(ctx context.Context) error {
	now := time.Now().UTC()
	if err := s.store.RetireActive(ctx, now, RotationOverlap); err != nil {
		return fmt.Errorf("jwks: retire: %w", err)
	}
	key, err := GenerateKey(s.algorithm)
	if err != nil {
		return err
	}
	return s.insertKey(ctx, key)
}

// insertKey encrypts the private PEM and persists the pair.
func (s *Service) insertKey(ctx context.Context, key *GeneratedKey) error {
	sealed, err := s.cipher.Encrypt(string(key.PrivatePEM))
	if err != nil {
		return fmt.Errorf("jwks: encrypt private key: %w", err)
	}
	if err := s.store.Insert(ctx, key, []byte(sealed)); err != nil {
		return fmt.Errorf("jwks: insert key: %w", err)
	}
	return nil
}

// SignKey returns the active signing key as a jwk.Key with kid and
// alg bound. Implements jwtutils.KeyProvider.
func (s *Service) SignKey(ctx context.Context) (jwk.Key, error) {
	stored, err := s.store.ActiveSigningKey(ctx)
	if err != nil {
		return nil, err
	}

	plaintext, err := s.decryptPrivate(stored.PrivatePEM)
	if err != nil {
		return nil, err
	}
	raw, err := parsePrivatePEM(plaintext)
	if err != nil {
		return nil, err
	}
	return bindKey(raw, stored.KeyID, stored.Algorithm)
}

// VerifyKeySet builds the published public key set, retired keys
// included until their overlap expires. Implements
// jwtutils.KeyProvider.
func (s *Service) VerifyKeySet(ctx context.Context) (jwk.Set, error) {
	stored, err := s.store.PublishedKeys(ctx)
	if err != nil {
		return nil, err
	}

	set := jwk.NewSet()
	for _, key := range stored {
		raw, parseErr := parsePublicPEM(key.PublicPEM)
		if parseErr != nil {
			return nil, fmt.Errorf("jwks: parse public key %s: %w", key.KeyID, parseErr)
		}
		pub, bindErr := bindKey(raw, key.KeyID, key.Algorithm)
		if bindErr != nil {
			return nil, bindErr
		}
		if err := set.AddKey(pub); err != nil {
			return nil, fmt.Errorf("jwks: add key %s: %w", key.KeyID, err)
		}
	}
	return set, nil
}

// decryptPrivate opens the sealed private PEM.
func (s *Service) decryptPrivate(sealed []byte) ([]byte, error) {
	plaintext, err := s.cipher.Decrypt(string(sealed))
	if err != nil {
		return nil, fmt.Errorf("jwks: decrypt private key: %w", err)
	}
	return []byte(plaintext), nil
}

// bindKey wraps a raw crypto key into a jwk.Key carrying kid + alg.
func bindKey(raw any, keyID, algorithm string) (jwk.Key, error) {
	key, err := jwk.Import(raw)
	if err != nil {
		return nil, fmt.Errorf("jwks: import key: %w", err)
	}
	if err := key.Set(jwk.KeyIDKey, keyID); err != nil {
		return nil, fmt.Errorf("jwks: set kid: %w", err)
	}
	if err := key.Set(jwk.AlgorithmKey, jwa.NewSignatureAlgorithm(algorithm)); err != nil {
		return nil, fmt.Errorf("jwks: set alg: %w", err)
	}
	return key, nil
}
