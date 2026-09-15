package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidKeySize reports a key that is not 32 bytes.
var ErrInvalidKeySize = errors.New("crypto: AES-256 requires a 32-byte key")

// ErrCiphertextTooShort reports a value shorter than the nonce prefix.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext is too short")

// ErrMissingPrefix reports a recoverable value stored without the
// enc: marker.
var ErrMissingPrefix = errors.New("crypto: value is missing the enc: prefix")

// Cipher encrypts and decrypts values with AES-256-GCM.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte key.
func NewCipher(key []byte) (*Cipher, error) {
	if len(key) != 32 {
		return nil, ErrInvalidKeySize
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES block: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create GCM: %w", err)
	}
	return &Cipher{aead: aead}, nil
}

// EncPrefix marks a recoverable value sealed by this package; every
// stored ciphertext must carry it. Values without the prefix are
// invalid — there is no unprefixed legacy format.
const EncPrefix = "enc:"

// Encrypt seals plaintext with a fresh random nonce and returns the
// canonical "enc:<ciphertext>" form.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	rand.Read(nonce) // never returns an error per the crypto/rand contract

	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return EncPrefix + base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt. Values without the
// enc: prefix are rejected.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	rest, ok := strings.CutPrefix(encoded, EncPrefix)
	if !ok {
		return "", ErrMissingPrefix
	}
	data, err := base64.RawStdEncoding.DecodeString(rest)
	if err != nil {
		return "", fmt.Errorf("decode ciphertext: %w", err)
	}
	if len(data) < c.aead.NonceSize() {
		return "", ErrCiphertextTooShort
	}

	nonce, ciphertext := data[:c.aead.NonceSize()], data[c.aead.NonceSize():]
	plaintext, err := c.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(plaintext), nil
}
