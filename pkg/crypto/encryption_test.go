package crypto

import (
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

	decrypted, err := cipher.Decrypt(encrypted)
	require.NoError(t, err)
	assert.Equal(t, "s3cret-value-€", decrypted)
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

	// Flip one ciphertext byte: GCM authentication must fail.
	raw := []byte(encrypted)
	raw[len(raw)-1] ^= 0x01

	_, err = cipher.Decrypt(string(raw))
	assert.Error(t, err)
}

func TestCipherRejectsShortCiphertext(t *testing.T) {
	cipher, err := NewCipher(testKey())
	require.NoError(t, err)

	_, err = cipher.Decrypt("AAAA")
	assert.ErrorIs(t, err, ErrCiphertextTooShort)
}

func TestNewCipherRejectsWrongKeySize(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		_, err := NewCipher(make([]byte, size))
		assert.ErrorIs(t, err, ErrInvalidKeySize, size)
	}
}
