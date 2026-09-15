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
	// AlgorithmScrypt is the default password hashing algorithm.
	AlgorithmScrypt Algorithm = "scrypt"

	// AlgorithmArgon2id selects Argon2id.
	AlgorithmArgon2id Algorithm = "argon2id"
)

// Default hashing costs.
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

// PasswordHasher hashes and verifies passwords with scrypt or Argon2id.
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

// WithAlgorithm selects the hashing scheme.
func (h *PasswordHasher) WithAlgorithm(algorithm Algorithm) *PasswordHasher {
	clone := *h
	clone.algorithm = algorithm
	return &clone
}

// WithScryptCost sets the scrypt cost parameters.
func (h *PasswordHasher) WithScryptCost(n, r uint32, p uint8) *PasswordHasher {
	clone := *h
	clone.scryptN, clone.scryptR, clone.scryptP = n, r, p
	return &clone
}

// WithArgon2Cost sets the Argon2id memory, iteration, and lane counts.
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

// Hash derives a PHC-formatted password hash with a fresh random salt.
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

// Verify reports whether password matches the encoded hash.
func (h *PasswordHasher) Verify(password, encoded string) (bool, error) {
	cost, salt, want, err := decodePasswordHash(encoded)
	if err != nil {
		return false, err
	}

	got, err := cost.derive([]byte(password), salt, uint32(len(want))) //nolint:gosec // key length comes from the decoded hash segment, bounded well below 2^32
	if err != nil {
		return false, err
	}

	var match bool
	subtle.WithDataIndependentTiming(func() {
		match = subtle.ConstantTimeCompare(got, want) == 1
	})
	return match, nil
}

type cost interface {
	derive(password, salt []byte, keyLength uint32) ([]byte, error)
}

// argon2Cost holds Argon2id parameters.
type argon2Cost struct {
	memoryKiB, iterations uint32
	threads               uint8
}

func (c argon2Cost) derive(password, salt []byte, keyLength uint32) ([]byte, error) {
	return argon2.IDKey(password, salt, c.iterations, c.memoryKiB, c.threads, keyLength), nil
}

// scryptCost holds scrypt parameters.
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

// Errors returned while decoding password hashes.
var (
	ErrUnknownAlgorithm = errors.New("crypto: unknown algorithm")
	ErrInvalidHash      = errors.New("crypto: invalid hash encoding")
)

// decodePasswordHash parses a PHC string.
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

// decodeArgon2Cost parses Argon2id parameters.
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

// decodeScryptCost parses scrypt parameters.
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

// costValue parses a uint32 cost value.
func costValue(s string) (uint32, error) {
	value, err := parseCost(s, 32)
	return uint32(value), err //nolint:gosec // ParseUint with bitSize 32 bounds the value
}

// costUint8 parses a uint8 cost value.
func costUint8(s string) (uint8, error) {
	value, err := parseCost(s, 8)
	return uint8(value), err //nolint:gosec // ParseUint with bitSize 8 bounds the value
}

// parseCost parses an unsigned cost value with the given bit size.
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
