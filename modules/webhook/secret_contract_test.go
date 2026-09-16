package webhook

// secret_contract_test.go pins the recoverable-secret storage
// contract: stored secrets carry the enc: marker, unprefixed values
// are rejected by the database, and the ciphertext never reaches an
// API response.

import (
	"strings"
	"testing"

	"github.com/riipandi/tango/pkg/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoredSecretCarriesEncPrefix(t *testing.T) {
	stack := newTestStack(t, okSender())

	created := stack.create(t, "enc-hook", "https://receiver.example/hook")

	// The plaintext is shown exactly once at creation; the stored
	// value is the enc:-prefixed ciphertext.
	require.NotNil(t, created.Secret)
	plaintext := *created.Secret
	require.False(t, strings.HasPrefix(plaintext, crypto.EncPrefix), "the response carries the plaintext, not ciphertext")

	var stored string
	require.NoError(t, stack.DB.QueryRow(t.Context(),
		"SELECT secret FROM public.webhook_events WHERE id = $1", created.ID.UUID()).Scan(&stored))
	require.True(t, strings.HasPrefix(stored, crypto.EncPrefix), "stored secret must carry the enc: prefix")

	// The service round-trips the value for signing.
	plain, err := stack.Service.cipher.Decrypt(stored)
	require.NoError(t, err)
	assert.Equal(t, plaintext, plain)
}

func TestUnprefixedSecretIsRejected(t *testing.T) {
	stack := newTestStack(t, okSender())

	_, err := stack.DB.Exec(t.Context(),
		`INSERT INTO public.webhook_events (name, endpoint, method, secret, event_types) VALUES ('unprefixed', 'https://x.example', 'POST', 'plain-secret', '{}')`)
	require.Error(t, err, "the database must reject an unprefixed secret")
	assert.Contains(t, err.Error(), "chk_webhook_secret_enc")
}
