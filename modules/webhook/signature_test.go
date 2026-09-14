package webhook

import (
	"testing"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixedTime pins the signed timestamp so the header value is a stable
// vector a receiver implementation can be tested against.
var fixedTime = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)

func TestSignProducesVerifiableDelivery(t *testing.T) {
	payload := map[string]any{"event": "user.created", "user_id": "user_01m2"}

	delivery, err := Sign("https://example.test/hook", "POST",
		map[string]string{"X-Custom": "1"}, "user.created", payload, "s3cret", fixedTime)
	require.NoError(t, err)

	assert.Equal(t, "https://example.test/hook", delivery.URL)
	assert.Equal(t, "POST", delivery.Method)
	assert.Equal(t, "1", delivery.Headers["X-Custom"])
	assert.Equal(t, "application/json", delivery.Headers["Content-Type"])
	assert.Equal(t, "user.created", delivery.Headers[EventHeader])

	// The body is canonical JSON: the receiver re-serializes and
	// compares bytes, so key order must be deterministic.
	var decoded map[string]any
	require.NoError(t, jsonv2.Unmarshal(delivery.Body, &decoded))
	assert.Equal(t, "user.created", decoded["event"])

	require.NoError(t, VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, "s3cret", fixedTime))
}

func TestSignSignatureVector(t *testing.T) {
	// Pinned vector: timestamp + canonical body + HMAC-SHA256.
	// Recomputing this value by hand must reproduce it, which is what
	// a third-party receiver verifier asserts against.
	delivery, err := Sign("https://example.test/hook", "POST", nil, "user.created",
		map[string]any{"event": "user.created"}, "s3cret", fixedTime)
	require.NoError(t, err)

	assert.Equal(t, `{"event":"user.created"}`, string(delivery.Body))
	assert.Regexp(t, `^t=1789387200,v1=[0-9a-f]{64}$`, delivery.Headers[SignatureHeader])
}

func TestSignCanonicalPayloadStableAcrossCalls(t *testing.T) {
	payload := map[string]any{"b": 2, "a": 1, "nested": map[string]any{"z": true, "y": "x"}}

	first, err := CanonicalPayload(payload)
	require.NoError(t, err)
	second, err := CanonicalPayload(payload)
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second), "canonical encoding must be reproducible")
}

func TestVerifySignatureRejectsTamperedBody(t *testing.T) {
	delivery, err := Sign("https://example.test/hook", "POST", nil, "user.created",
		map[string]any{"event": "user.created"}, "s3cret", fixedTime)
	require.NoError(t, err)

	tampered := append([]byte{}, delivery.Body...)
	tampered[0] = '['

	err = VerifySignature(delivery.Headers[SignatureHeader], tampered, "s3cret", fixedTime)
	assert.ErrorContains(t, err, "signature mismatch")
}

func TestVerifySignatureRejectsWrongSecret(t *testing.T) {
	delivery, err := Sign("https://example.test/hook", "POST", nil, "user.created",
		map[string]any{"event": "user.created"}, "s3cret", fixedTime)
	require.NoError(t, err)

	err = VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, "other", fixedTime)
	assert.ErrorContains(t, err, "signature mismatch")
}

func TestVerifySignatureRejectsStaleTimestamp(t *testing.T) {
	delivery, err := Sign("https://example.test/hook", "POST", nil, "user.created",
		map[string]any{"event": "user.created"}, "s3cret", fixedTime)
	require.NoError(t, err)

	// Replay beyond the tolerance window fails even with a valid MAC.
	err = VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, "s3cret", fixedTime.Add(10*time.Minute))
	assert.ErrorContains(t, err, "outside the tolerance window")

	// Inside the window it still verifies.
	require.NoError(t, VerifySignature(delivery.Headers[SignatureHeader], delivery.Body, "s3cret", fixedTime.Add(time.Minute)))
}

func TestVerifySignatureRejectsMalformedHeader(t *testing.T) {
	for _, header := range []string{"", "v1=abc", "t=123", "garbage", "t=abc,v1=def"} {
		err := VerifySignature(header, []byte("{}"), "s3cret", fixedTime)
		assert.Error(t, err, "header %q must be rejected", header)
	}
}

func TestCanonicalPayloadRejectsOversizedEvent(t *testing.T) {
	payload := map[string]any{"blob": make([]byte, maxPayloadBytes+1)}
	_, err := CanonicalPayload(payload)
	assert.ErrorIs(t, err, ErrTooLarge)
}

func TestSubscribedTo(t *testing.T) {
	cases := []struct {
		name     string
		events   []string
		event    string
		expected bool
	}{
		{"empty subscribes to everything", nil, "user.created", true},
		{"wildcard subscribes to everything", []string{AllEvents}, "anything", true},
		{"exact match", []string{"user.created"}, "user.created", true},
		{"no match", []string{"user.created"}, "user.deleted", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hook := Webhook{EventTypes: tc.events}
			assert.Equal(t, tc.expected, hook.SubscribedTo(tc.event))
		})
	}
}

func TestNormalizeEventTypes(t *testing.T) {
	got := normalizeEventTypes([]string{" user.deleted ", "user.created", "user.created", ""})
	assert.Equal(t, []string{"user.created", "user.deleted"}, got)
	assert.Equal(t, []string{}, normalizeEventTypes(nil))
}
