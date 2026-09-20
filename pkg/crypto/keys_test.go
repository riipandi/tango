package crypto

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeyHexLength(t *testing.T) {
	first, err := GenerateKeyHex()
	require.NoError(t, err)
	assert.Len(t, first, KeyHexLength)

	second, err := GenerateKeyHex()
	require.NoError(t, err)
	assert.NotEqual(t, first, second, "keys must not repeat")
}

func TestGenerateRandomHex(t *testing.T) {
	value, err := GenerateRandomHex(48)
	require.NoError(t, err)
	assert.Len(t, value, 96)

	_, err = GenerateRandomHex(0)
	assert.Error(t, err)
}

func TestParseKeyHex(t *testing.T) {
	encoded, err := GenerateKeyHex()
	require.NoError(t, err)

	key, err := ParseKeyHex(encoded)
	require.NoError(t, err)
	assert.Len(t, key, KeySize)
	assert.Equal(t, encoded, hex.EncodeToString(key))

	_, err = ParseKeyHex("not-hex")
	assert.ErrorIs(t, err, ErrInvalidKeyEncoding)

	_, err = ParseKeyHex(strings.Repeat("ab", KeySize-1))
	assert.ErrorIs(t, err, ErrInvalidKeyEncoding)
}

func TestNewCipherFromHexRoundTrip(t *testing.T) {
	encoded, err := GenerateKeyHex()
	require.NoError(t, err)

	cipher, err := NewCipherFromHex(encoded)
	require.NoError(t, err)

	sealed, err := cipher.Encrypt("secret")
	require.NoError(t, err)

	opened, err := cipher.Decrypt(sealed)
	require.NoError(t, err)
	assert.Equal(t, "secret", opened)
}
