// Package jwks publishes the JSON Web Key Set the application signs with and
// verifies against.
//
// Two sources feed one set. The configuration holds the application's own key
// pair (`auth.private_key` / `auth.public_key`), which is the default signing
// key for stateless JWTs. The database holds the keys of a deployment that
// acts as an OAuth provider, which is how a second key joins the set without
// a redeploy.
//
// The service satisfies jwtutils.KeyProvider, so the endpoint that publishes
// the set and the code that verifies a token read the same source: a key that
// is not published is not accepted, and a key that is published is accepted
// without a second list to keep in step.
package jwks

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/lestrrat-go/jwx/v3/jwk"

	"github.com/riipandi/tango/internal/config"
)

// ErrNoSigningKey reports a configuration with no private key to sign with.
// The HMAC-only configuration is a valid one, so this is reported by the
// caller that needs a key pair rather than refused at start-up.
var ErrNoSigningKey = errors.New("jwks: no signing key configured")

// Source reads the published keys a deployment stores itself.
type Source interface {
	ActiveSigningKeys(ctx context.Context) ([]StoredKey, error)
}

// Service owns the published key set.
type Service struct {
	source Source
	log    *slog.Logger

	// once guards the parse of the configured key pair: it is read from a
	// string that does not change for the life of the process, and a parse
	// failure is remembered rather than retried on every request.
	once       sync.Once
	privateKey jwk.Key
	publicKey  jwk.Key
	parseErr   error
}

// NewService builds the service. source may be nil, which is a run with no
// OAuth provider tables to read: the published set is then the configured key
// alone.
func NewService(cfg config.Config, source Source, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	service := &Service{source: source, log: log}
	// The parse runs here rather than on the first request: a key that cannot
	// be read is a broken deployment, and it should be reported before the
	// listener opens instead of as a 500 on a client's first verification.
	service.parse(cfg)
	return service
}

// parse reads the configured key pair once. A missing pair is not an error —
// a deployment that signs with the HMAC secret alone has none — but a pair
// that is present and unreadable is.
func (s *Service) parse(cfg config.Config) {
	s.once.Do(func() {
		if cfg.Auth.PublicKey == "" {
			return
		}
		public, err := decodeKey(cfg.Auth.PublicKey)
		if err != nil {
			s.parseErr = fmt.Errorf("jwks: auth.public_key: %w", err)
			return
		}
		// The `use` is stamped here, once, rather than at publish time: the
		// key is shared by every request, so mutating it while serving would
		// be a data race.
		if setErr := public.Set(jwk.KeyUsageKey, KeyUsageSignature); setErr != nil {
			s.parseErr = fmt.Errorf("jwks: auth.public_key: set use: %w", setErr)
			return
		}
		s.publicKey = public

		if cfg.Auth.PrivateKey == "" {
			return
		}
		private, err := decodeKey(cfg.Auth.PrivateKey)
		if err != nil {
			s.parseErr = fmt.Errorf("jwks: auth.private_key: %w", err)
			return
		}
		s.privateKey = private
	})
}

// Err reports the parse failure of the configured key pair, if any.
func (s *Service) Err() error { return s.parseErr }

// SignKey returns the configured private key. It is the application's own
// key, so stateless JWTs are signed with it in every deployment, whether or
// not the database holds more keys.
func (s *Service) SignKey(context.Context) (jwk.Key, error) {
	if s.parseErr != nil {
		return nil, s.parseErr
	}
	if s.privateKey == nil {
		return nil, ErrNoSigningKey
	}
	return s.privateKey, nil
}

// VerifyKeySet returns the public keys a token may be verified against: the
// configured public key first, then every active signing key the database
// holds.
//
// A database that cannot be read degrades the set to the configured key
// rather than failing the call. The configured key is the one the application
// signs with, so a caller verifying its own token still succeeds while the
// provider tables are unavailable; the alternative — refusing every
// verification because a future feature's table is unreachable — would turn a
// partial outage into a total one. The failure is logged, not swallowed.
func (s *Service) VerifyKeySet(ctx context.Context) (jwk.Set, error) {
	if s.parseErr != nil {
		return nil, s.parseErr
	}

	set := jwk.NewSet()
	seen := make(map[string]struct{}, 1)

	if s.publicKey != nil {
		if err := set.AddKey(s.publicKey); err != nil {
			return nil, fmt.Errorf("jwks: add configured key: %w", err)
		}
		if kid, ok := s.publicKey.KeyID(); ok {
			seen[kid] = struct{}{}
		}
	}

	stored, err := s.storedKeys(ctx)
	if err != nil {
		s.log.ErrorContext(ctx, "jwks: reading stored keys failed; publishing the configured key alone", "err", err)
		return set, nil
	}

	for _, key := range stored {
		// The configured key is the default, so a database row carrying the
		// same `kid` does not replace it and does not appear twice: a set
		// with one key named twice is a set a client cannot index.
		if _, duplicate := seen[key.KeyID]; duplicate {
			continue
		}
		parsed, err := parseStoredKey(key)
		if err != nil {
			// One unusable row must not take the whole set down: the other
			// keys are still the ones a client needs.
			s.log.ErrorContext(ctx, "jwks: skipping an unusable stored key",
				"kid", key.KeyID, "err", err)
			continue
		}
		if err := set.AddKey(parsed); err != nil {
			return nil, fmt.Errorf("jwks: add stored key %s: %w", key.KeyID, err)
		}
		seen[key.KeyID] = struct{}{}
	}
	return set, nil
}

// storedKeys reads the database keys, or nothing when no source is wired.
func (s *Service) storedKeys(ctx context.Context) ([]StoredKey, error) {
	if s.source == nil {
		return nil, nil
	}
	return s.source.ActiveSigningKeys(ctx)
}

// parseStoredKey turns one stored row into a public JWK.
//
// The stored public key is JWK JSON, which is what the OAuth provider work
// writes. The private key column is never read, so a row that carries one
// cannot leak it here. The `kid`, `alg`, and `use` the row declares are
// stamped onto the key: a published JWK without them is one a client cannot
// match to a token header.
func parseStoredKey(key StoredKey) (jwk.Key, error) {
	if key.KeyID == "" {
		return nil, errors.New("key id is empty")
	}
	parsed, err := jwk.ParseKey(key.PublicKey)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}

	// A stored private key would parse as a private JWK; the public half is
	// what the set carries, so the private fields are dropped here whatever
	// the row holds.
	public, err := jwk.PublicKeyOf(parsed)
	if err != nil {
		return nil, fmt.Errorf("derive public key: %w", err)
	}
	if err := public.Set(jwk.KeyIDKey, key.KeyID); err != nil {
		return nil, fmt.Errorf("set kid: %w", err)
	}
	if key.Algorithm != "" {
		if err := public.Set(jwk.AlgorithmKey, key.Algorithm); err != nil {
			return nil, fmt.Errorf("set alg: %w", err)
		}
	}
	if err := public.Set(jwk.KeyUsageKey, KeyUsageSignature); err != nil {
		return nil, fmt.Errorf("set use: %w", err)
	}
	return public, nil
}

// decodeKey reads a base64 (raw, unpadded) JWK JSON value, the form
// pkg/crypto's KeyGenerator writes and the env file carries.
func decodeKey(encoded string) (jwk.Key, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	key, err := jwk.ParseKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse JWK: %w", err)
	}
	return key, nil
}
