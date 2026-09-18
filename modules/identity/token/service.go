package token

import (
	"crypto/rand"
	"encoding/base64"
)

// NewRaw draws a fresh opaque 256-bit token, base64url-encoded.
// Callers store token.Hash(raw) and deliver raw once.
func NewRaw() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
