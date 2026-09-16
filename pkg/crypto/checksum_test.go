package crypto

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHMACRoundTrip(t *testing.T) {
	payload := []byte(`{"event":"webhook.delivered"}`)
	key := []byte("webhook-signing-secret")

	sig := SignHMAC(payload, key)
	require.Len(t, sig, 32)

	// The same payload and key reproduce the signature; any change
	// to either invalidates it.
	assert.Equal(t, sig, SignHMAC(payload, key))
	assert.NotEqual(t, sig, SignHMAC([]byte("tampered"), key))
	assert.NotEqual(t, sig, SignHMAC(payload, []byte("other-key")))
}
