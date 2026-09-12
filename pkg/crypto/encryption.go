package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
)

// ErrInvalidKeySize is returned when a Cipher key is not 32 bytes.
var ErrInvalidKeySize = errors.New("crypto: AES-256 requires a 32-byte key")

// ErrCiphertextTooShort is returned when a value is too short to
// contain the nonce prefix of a sealed ciphertext.
var ErrCiphertextTooShort = errors.New("crypto: ciphertext is too short")

// Cipher encrypts and decrypts secrets at rest (app settings, MFA
// secrets, JWKS private keys) with AES-256-GCM. Wire format is
// nonce-first, base64 encoded: base64(nonce || ciphertext || tag).
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a 32-byte key. The key comes from
// configuration; derive it (e.g. SHA-256 of a master secret) when the
// source material is not exactly 32 bytes.
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

// Encrypt seals plaintext under a fresh random nonce. Encrypted
// values are safe to store per-column; the nonce never repeats.
func (c *Cipher) Encrypt(plaintext string) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	rand.Read(nonce) // never returns an error per the crypto/rand contract

	sealed := c.aead.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

// Decrypt opens a value produced by Encrypt. Authentication failure
// (wrong key or tampered value) surfaces as an error.
func (c *Cipher) Decrypt(encoded string) (string, error) {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
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
