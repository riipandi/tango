package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChecksumBlake3(t *testing.T) {
	// Official BLAKE3 vector: hash of the empty input.
	checksum, err := ChecksumBlake3(nil, Blake3Output32)

	require.NoError(t, err)
	assert.Equal(t,
		"af1349b9f5f9a1a6a0404dea36dcc9499bcb25c9adc112b7cc9a93cae41f3262",
		checksumHex(checksum),
	)
}

func TestChecksumBlake3ExtendsAndTruncates(t *testing.T) {
	data := []byte("abc")

	base, err := ChecksumBlake3Hex(data, Blake3Output32)
	require.NoError(t, err)

	// Lengths 8 and 16 are prefixes of the 32-byte digest; 64 starts
	// with the same 32 bytes (BLAKE3's streamable output).
	short8, err := ChecksumBlake3Hex(data, Blake3Output8)
	require.NoError(t, err)
	assert.Equal(t, base[:16], short8)

	short16, err := ChecksumBlake3Hex(data, Blake3Output16)
	require.NoError(t, err)
	assert.Equal(t, base[:32], short16)

	long64, err := ChecksumBlake3Hex(data, Blake3Output64)
	require.NoError(t, err)
	assert.Equal(t, base, long64[:64])
}

func TestChecksumBlake3RejectsInvalidLength(t *testing.T) {
	for _, length := range []Blake3OutputLength{0, 7, 24, 128} {
		_, err := ChecksumBlake3([]byte("abc"), length)
		assert.ErrorIs(t, err, ErrInvalidBlake3OutputLength, length)
	}
}

func TestHMACRoundTrip(t *testing.T) {
	payload := []byte(`{"event":"webhook.delivered"}`)
	key := []byte("webhook-signing-secret")

	sig := SignHMAC(payload, key)

	assert.True(t, VerifyHMAC(payload, sig, key))
	assert.False(t, VerifyHMAC(payload, sig, []byte("other-secret")))
	assert.False(t, VerifyHMAC(append(payload, ' '), sig, key))
	assert.False(t, VerifyHMAC(payload, sig[:len(sig)-1], key))
}

func checksumHex(b []byte) string {
	return hex.EncodeToString(b)
}
