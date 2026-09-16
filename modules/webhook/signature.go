package webhook

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/pkg/crypto"
)

// Signature header shape, mirroring the widely deployed
// "t=timestamp,v1=hexdigest" scheme: the timestamp binds the signature
// to a moment so a captured body cannot be replayed later.
const (
	// SignatureHeader carries the signed delivery timestamp + digest.
	SignatureHeader = "X-Signature"
	// EventHeader names the event that triggered the delivery.
	EventHeader = "X-Webhook-Event"
	// DeliveryHeader identifies the endpoint the body was sent for.
	DeliveryHeader = "X-Webhook-Id"
	// SignatureVersion prefixes the digest inside the header.
	SignatureVersion = "v1"
	// maxSignatureSkew is how far the signed timestamp may sit from
	// the local clock before verification fails.
	maxSignatureSkew = 5 * time.Minute
)

// Delivery is one signed webhook request, already rendered.
type OutboundDelivery struct {
	// URL is the registered endpoint.
	URL string
	// Method is the registered HTTP verb.
	Method string
	// Headers are the registered custom headers plus the signature set.
	Headers map[string]string
	// Body is the canonical JSON encoding of the event payload.
	Body []byte
}

// Sign renders one outbound request: the signature covers a timestamp
// prefix concatenated with the exact committed body bytes.
func Sign(event, endpoint, method string, custom map[string]string, body []byte, secret string, at time.Time) (OutboundDelivery, error) {
	timestamp := strconv.FormatInt(at.Unix(), 10)
	headers := make(map[string]string, len(custom)+4)
	for name, value := range custom {
		headers[name] = value
	}
	headers["Content-Type"] = "application/json"
	headers[EventHeader] = event
	headers[SignatureHeader] = SignatureHeaderValue(timestamp, body, secret)

	return OutboundDelivery{URL: endpoint, Method: method, Headers: headers, Body: body}, nil
}

// CanonicalPayload encodes an event payload for signing. Deterministic
// ordering makes the bytes reproducible for the same logical payload —
// the receiver can verify over exactly what it received. encoding/json/v2
// does not sort map keys by default, so the option is required here.
func CanonicalPayload(payload map[string]any) ([]byte, error) {
	body, err := jsonv2.Marshal(payload, jsonv2.Deterministic(true))
	if err != nil {
		return nil, fmt.Errorf("webhook: encode payload: %w", err)
	}
	if len(body) > maxPayloadBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(body))
	}
	return body, nil
}

// SignatureHeaderValue builds "t=<unix>,v1=<hex hmac>". The signed
// message is "<unix>.<body>": the timestamp is authenticated without
// being re-serialized, and short hex avoids a signed-digit attack.
func SignatureHeaderValue(timestamp string, body []byte, secret string) string {
	mac := crypto.SignHMAC(signedMessage(timestamp, body), []byte(secret))
	return "t=" + timestamp + "," + SignatureVersion + "=" + fmt.Sprintf("%x", mac)
}

// VerifySignature reports whether header authenticates body under
// secret. Empty signature header or a stale timestamp fails.
func VerifySignature(header string, body []byte, secret string, now time.Time) error {
	timestamp, digest, err := parseSignatureHeader(header)
	if err != nil {
		return err
	}

	signedAt, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return errors.New("webhook: signature timestamp is not a unix time")
	}
	if drift := now.Sub(time.Unix(signedAt, 0)); drift > maxSignatureSkew || drift < -maxSignatureSkew {
		return errors.New("webhook: signature timestamp is outside the tolerance window")
	}

	expected := SignatureHeaderValue(timestamp, body, secret)
	if digest != sigDigest(expected) {
		return errors.New("webhook: signature mismatch")
	}
	return nil
}

// sigDigest extracts the hex digest from a rendered header value.
func sigDigest(header string) string {
	_, digest, err := parseSignatureHeader(header)
	if err != nil {
		return ""
	}
	return digest
}

// parseSignatureHeader splits "t=<unix>,v1=<hex>" into its parts.
func parseSignatureHeader(header string) (timestamp, digest string, err error) {
	for part := range bytes.SplitSeq([]byte(header), []byte(",")) {
		name, value, found := bytes.Cut(part, []byte("="))
		if !found {
			continue
		}
		switch string(name) {
		case "t":
			timestamp = string(value)
		case SignatureVersion:
			digest = string(value)
		}
	}
	if timestamp == "" || digest == "" {
		return "", "", errors.New("webhook: malformed signature header")
	}
	return timestamp, digest, nil
}

// signedMessage is the byte string the HMAC covers.
func signedMessage(timestamp string, body []byte) []byte {
	message := make([]byte, 0, len(timestamp)+1+len(body))
	message = append(message, timestamp...)
	message = append(message, '.')
	return append(message, body...)
}
