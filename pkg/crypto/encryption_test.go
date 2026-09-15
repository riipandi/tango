package crypto

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestCipherRoundTrip(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	encrypted, err := cipher.Encrypt("s3cret-value-€")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(encrypted, EncPrefix), "ciphertext must carry the enc: prefix")

	decrypted, err := cipher.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, "s3cret-value-€", decrypted)
}

func TestCipherRejectsUnprefixedValue(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	// Raw base64 without the enc: marker is invalid — there is no
	// legacy format and no fallback decoding.
	_, err = cipher.Decrypt("c2VjcmV0LXZhbHVlLWV1cm8")
	assert.ErrorIs(t, err, ErrMissingPrefix)
}

func TestCipherUsesFreshNonce(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	first, err := cipher.Encrypt("same plaintext")
	require.NoError(t, err)
	second, err := cipher.Encrypt("same plaintext")
	require.NoError(t, err)

	assert.NotEqual(t, first, second, "nonce must never repeat")
}

func TestCipherRejectsTampering(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	encrypted, err := cipher.Encrypt("integrity matters")
	require.NoError(t, err)

	raw := []byte(strings.TrimPrefix(encrypted, EncPrefix))
	raw[len(raw)-1] ^= 0x01

	_, err = cipher.Decrypt(EncPrefix + string(raw))
	assert.Error(t, err)
}

func TestCipherRejectsShortCiphertext(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	// A valid prefix wrapping a too-short payload still fails the
	// length check.
	_, err = cipher.Decrypt(EncPrefix + "AAAA")
	assert.ErrorIs(t, err, ErrCiphertextTooShort)
}

func TestNewCipherRejectsWrongKeySize(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		_, err := NewCipher(make([]byte, size))
		assert.ErrorIs(t, err, ErrInvalidKeySize, size)
	}
}
