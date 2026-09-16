package crypto

import (
	"crypto/hmac"
	"crypto/sha256"
)

// SignHMAC returns the HMAC-SHA256 signature of payload under key.
func SignHMAC(payload, key []byte) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return mac.Sum(nil)
}
