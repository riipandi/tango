// Package crypto provides the platform's cryptographic
// primitives: BLAKE3 payload checksums, HMAC-SHA256 signatures,
// AES-256-GCM encryption for secrets at rest, and configurable
// password hashing (scrypt by default, argon2id optional) in PHC
// format.
package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"github.com/zeebo/blake3"
)

// Blake3OutputLength represents valid output lengths for BLAKE3.
type Blake3OutputLength int

const (
	Blake3Output8  Blake3OutputLength = 8
	Blake3Output16 Blake3OutputLength = 16
	Blake3Output32 Blake3OutputLength = 32
	Blake3Output64 Blake3OutputLength = 64
)

// ErrInvalidBlake3OutputLength is returned when a checksum requests
// an output length outside the supported set.
var ErrInvalidBlake3OutputLength = errors.New("crypto: invalid blake3 output length")

// ChecksumBlake3 returns the BLAKE3 checksum of data with the given
// output length. Lengths below 32 truncate the first block; 64
// extends through BLAKE3's XOF.
func ChecksumBlake3(data []byte, length Blake3OutputLength) ([]byte, error) {
	if !validBlake3OutputLength(length) {
		return nil, fmt.Errorf("%w: %d", ErrInvalidBlake3OutputLength, length)
	}

	hasher := blake3.New()
	_, _ = hasher.Write(data) // never fails: in-memory hash state
	checksum := make([]byte, length)
	if _, err := io.ReadFull(hasher.Digest(), checksum); err != nil {
		return nil, fmt.Errorf("blake3: %w", err)
	}
	return checksum, nil
}

// ChecksumBlake3Hex returns the hex-encoded BLAKE3 checksum of data.
func ChecksumBlake3Hex(data []byte, length Blake3OutputLength) (string, error) {
	checksum, err := ChecksumBlake3(data, length)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(checksum), nil
}

func validBlake3OutputLength(length Blake3OutputLength) bool {
	switch length {
	case Blake3Output8, Blake3Output16, Blake3Output32, Blake3Output64:
		return true
	default:
		return false
	}
}

// SignHMAC returns the HMAC-SHA256 signature of payload under key —
// used for webhook deliveries and other signed payloads.
func SignHMAC(payload, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}

// VerifyHMAC reports whether sig is the valid HMAC-SHA256 of payload
// under key, comparing in constant time.
func VerifyHMAC(payload, sig, key []byte) bool {
	return hmac.Equal(sig, SignHMAC(payload, key))
}
