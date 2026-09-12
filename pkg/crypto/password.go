package crypto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/scrypt"
)

// Algorithm selects the password hashing scheme.
type Algorithm string

const (
	// AlgorithmScrypt is the default: memory-hard scrypt with
	// N=2^16, r=8, p=1 (64 MiB per hash).
	AlgorithmScrypt Algorithm = "scrypt"

	// AlgorithmArgon2id is the PHC winner: m=64 MiB, t=3, p=4.
	AlgorithmArgon2id Algorithm = "argon2id"
)

// Default costs; override per environment through the builder.
const (
	defaultScryptN          = 1 << 16 // 64 MiB with r=8
	defaultScryptR          = 8
	defaultScryptP          = 1
	defaultArgon2MemoryKiB  = 64 * 1024
	defaultArgon2Iterations = 3
	defaultArgon2Threads    = 4
	defaultKeyLength        = 64
	defaultSaltLength       = 16
)

// PasswordHasher hashes and verifies passwords with scrypt or
// argon2id. Hashes are emitted in PHC string format so the algorithm
// and cost travel with every value — verification reads them from the
// hash itself, letting costs evolve without a data migration.
type PasswordHasher struct {
	algorithm             Algorithm
	scryptN, scryptR      uint32
	scryptP               uint8
	argon2MemoryKiB       uint32
	argon2Iterations      uint32
	argon2Threads         uint8
	keyLength, saltLength uint32
}

// NewPasswordHasher returns a hasher with scrypt defaults.
func NewPasswordHasher() *PasswordHasher {
	return &PasswordHasher{
		algorithm:        AlgorithmScrypt,
		scryptN:          defaultScryptN,
		scryptR:          defaultScryptR,
		scryptP:          defaultScryptP,
		argon2MemoryKiB:  defaultArgon2MemoryKiB,
		argon2Iterations: defaultArgon2Iterations,
		argon2Threads:    defaultArgon2Threads,
		keyLength:        defaultKeyLength,
		saltLength:       defaultSaltLength,
	}
}

// WithAlgorithm switches the hashing scheme. Unknown values fail at
// Hash time.
func (h *PasswordHasher) WithAlgorithm(algorithm Algorithm) *PasswordHasher {
	clone := *h
	clone.algorithm = algorithm
	return &clone
}

// WithScryptCost sets the scrypt cost: N (must be a power of two),
// r, and p. Memory use is 128*N*r bytes.
func (h *PasswordHasher) WithScryptCost(n, r uint32, p uint8) *PasswordHasher {
	clone := *h
	clone.scryptN, clone.scryptR, clone.scryptP = n, r, p
	return &clone
}

// WithArgon2Cost sets the argon2id cost: memory in KiB, passes, and
// parallelism (lanes).
func (h *PasswordHasher) WithArgon2Cost(memoryKiB, iterations uint32, threads uint8) *PasswordHasher {
	clone := *h
	clone.argon2MemoryKiB, clone.argon2Iterations, clone.argon2Threads = memoryKiB, iterations, threads
	return &clone
}

// WithKeyLength sets the derived key length in bytes.
func (h *PasswordHasher) WithKeyLength(n uint32) *PasswordHasher {
	clone := *h
	clone.keyLength = n
	return &clone
}

// WithSaltLength sets the per-hash random salt length in bytes.
func (h *PasswordHasher) WithSaltLength(n uint32) *PasswordHasher {
	clone := *h
	clone.saltLength = n
	return &clone
}

// Hash derives the PHC-formatted hash of password using the
// configured algorithm and cost. Every call uses a fresh random salt,
// so equal passwords produce different hashes.
func (h *PasswordHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.saltLength)
	rand.Read(salt) // never returns an error per the crypto/rand contract

	switch h.algorithm {
	case AlgorithmArgon2id:
		key := argon2.IDKey([]byte(password), salt, h.argon2Iterations, h.argon2MemoryKiB, h.argon2Threads, h.keyLength)
		return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
			argon2.Version, h.argon2MemoryKiB, h.argon2Iterations, h.argon2Threads,
			base64.RawStdEncoding.EncodeToString(salt),
			base64.RawStdEncoding.EncodeToString(key)), nil

	case AlgorithmScrypt:
		key, err := scrypt.Key([]byte(password), salt, int(h.scryptN), int(h.scryptR), int(h.scryptP), int(h.keyLength))
		if err != nil {
			return "", fmt.Errorf("scrypt: %w", err)
		}
		return fmt.Sprintf("$scrypt$N=%d,r=%d,p=%d$%s$%s",
			h.scryptN, h.scryptR, h.scryptP,
			base64.RawStdEncoding.EncodeToString(salt),
			base64.RawStdEncoding.EncodeToString(key)), nil

	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownAlgorithm, h.algorithm)
	}
}

// Verify reports whether password matches the PHC-formatted hash.
// The algorithm and cost come from the hash, not the hasher config;
// malformed encodings are an error, mismatches are (false, nil).
func (h *PasswordHasher) Verify(password, encoded string) (bool, error) {
	cost, salt, want, err := decodePasswordHash(encoded)
	if err != nil {
		return false, err
	}

	got, err := cost.derive([]byte(password), salt, uint32(len(want))) //nolint:gosec // key length comes from the decoded hash segment, bounded well below 2^32
	if err != nil {
		return false, err
	}

	// Data-independent timing hardens the tag comparison on CPUs with
	// the feature; a no-op elsewhere (Go 1.26: inherited by spawned
	// goroutines, no more OS-thread pinning).
	var match bool
	subtle.WithDataIndependentTiming(func() {
		match = subtle.ConstantTimeCompare(got, want) == 1
	})
	return match, nil
}

// cost carries the parsed parameters of an encoded hash.
type cost interface {
	derive(password, salt []byte, keyLength uint32) ([]byte, error)
}

// argon2Cost implements cost for argon2id.
type argon2Cost struct {
	memoryKiB, iterations uint32
	threads               uint8
}

func (c argon2Cost) derive(password, salt []byte, keyLength uint32) ([]byte, error) {
	return argon2.IDKey(password, salt, c.iterations, c.memoryKiB, c.threads, keyLength), nil
}

// scryptCost implements cost for scrypt.
type scryptCost struct {
	n, r uint32
	p    uint8
}

func (c scryptCost) derive(password, salt []byte, keyLength uint32) ([]byte, error) {
	key, err := scrypt.Key(password, salt, int(c.n), int(c.r), int(c.p), int(keyLength))
	if err != nil {
		return nil, fmt.Errorf("scrypt: %w", err)
	}
	return key, nil
}

// Errors returned when decoding PHC-formatted hashes.
var (
	ErrUnknownAlgorithm = errors.New("crypto: unknown algorithm")
	ErrInvalidHash      = errors.New("crypto: invalid hash encoding")
)

// decodePasswordHash splits a PHC string into its cost parameters,
// salt, and derived key.
func decodePasswordHash(encoded string) (cost, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) < 5 || parts[0] != "" {
		return nil, nil, nil, fmt.Errorf("%w: want $algo$cost$salt$key", ErrInvalidHash)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[len(parts)-2])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: salt: %v", ErrInvalidHash, err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[len(parts)-1])
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%w: key: %v", ErrInvalidHash, err)
	}

	switch Algorithm(parts[1]) {
	case AlgorithmArgon2id:
		if len(parts) != 6 || !strings.HasPrefix(parts[2], "v=") {
			return nil, nil, nil, fmt.Errorf("%w: argon2id", ErrInvalidHash)
		}
		if version, err := costValue(parts[2]); err != nil || version != argon2.Version {
			return nil, nil, nil, fmt.Errorf("%w: unsupported argon2 version", ErrInvalidHash)
		}
		c, err := decodeArgon2Cost(parts[3])
		if err != nil {
			return nil, nil, nil, err
		}
		return c, salt, want, nil

	case AlgorithmScrypt:
		if len(parts) != 5 {
			return nil, nil, nil, fmt.Errorf("%w: scrypt", ErrInvalidHash)
		}
		c, err := decodeScryptCost(parts[2])
		if err != nil {
			return nil, nil, nil, err
		}
		return c, salt, want, nil

	default:
		return nil, nil, nil, fmt.Errorf("%w: %q", ErrUnknownAlgorithm, parts[1])
	}
}

// decodeArgon2Cost parses "m=<kib>,t=<passes>,p=<lanes>".
func decodeArgon2Cost(s string) (argon2Cost, error) {
	var c argon2Cost
	for part := range strings.SplitSeq(s, ",") {
		switch {
		case strings.HasPrefix(part, "m="):
			memoryKiB, err := costValue(part)
			if err != nil {
				return c, err
			}
			c.memoryKiB = memoryKiB
		case strings.HasPrefix(part, "t="):
			iterations, err := costValue(part)
			if err != nil {
				return c, err
			}
			c.iterations = iterations
		case strings.HasPrefix(part, "p="):
			threads, err := costUint8(part)
			if err != nil {
				return c, err
			}
			c.threads = threads
		default:
			return c, fmt.Errorf("%w: argon2 cost %q", ErrInvalidHash, part)
		}
	}
	return c, nil
}

// decodeScryptCost parses "N=<n>,r=<r>,p=<p>".
func decodeScryptCost(s string) (scryptCost, error) {
	var c scryptCost
	for part := range strings.SplitSeq(s, ",") {
		switch {
		case strings.HasPrefix(part, "N="):
			n, err := costValue(part)
			if err != nil {
				return c, err
			}
			c.n = n
		case strings.HasPrefix(part, "r="):
			r, err := costValue(part)
			if err != nil {
				return c, err
			}
			c.r = r
		case strings.HasPrefix(part, "p="):
			p, err := costUint8(part)
			if err != nil {
				return c, err
			}
			c.p = p
		default:
			return c, fmt.Errorf("%w: scrypt cost %q", ErrInvalidHash, part)
		}
	}
	return c, nil
}

// costValue parses a "<key>=<uint32>" cost segment.
func costValue(s string) (uint32, error) {
	value, err := parseCost(s, 32)
	return uint32(value), err //nolint:gosec // ParseUint with bitSize 32 bounds the value
}

// costUint8 parses a "<key>=<uint8>" cost segment (parallelism).
func costUint8(s string) (uint8, error) {
	value, err := parseCost(s, 8)
	return uint8(value), err //nolint:gosec // ParseUint with bitSize 8 bounds the value
}

// parseCost parses "<key>=<unsigned>" with the given bit size.
func parseCost(s string, bitSize int) (uint64, error) {
	_, raw, found := strings.Cut(s, "=")
	if !found {
		return 0, fmt.Errorf("%w: cost %q", ErrInvalidHash, s)
	}
	value, err := strconv.ParseUint(raw, 10, bitSize)
	if err != nil {
		return 0, fmt.Errorf("%w: cost %q", ErrInvalidHash, s)
	}
	return value, nil
}
