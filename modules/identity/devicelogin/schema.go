// Package devicelogin implements the QR / cross-device sign-in
// flow: an anonymous device parks a short-lived request keyed by a
// short user code; a signed-in user approves or denies it; the
// waiting device long-polls the exchange until approved.
package devicelogin

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// Request duration and polling cadence — upstream parity.
const (
	// RequestDuration bounds a pending login request.
	RequestDuration = 5 * time.Minute
	// PollingInterval is the client-side hint, in seconds.
	PollingInterval = 3
	// LongPollDuration bounds a single exchange call server-side.
	LongPollDuration = 25 * time.Second
	// PollTick is the server-side DB poll granularity.
	PollTick = 2 * time.Second

	// CodePrefix + code random length shape the user code ("P"+7).
	CodePrefix    = "P"
	CodeRandomLen = 7
)

// Request statuses (device_login_requests.status).
const (
	StatusPending  = "pending"
	StatusApproved = "approved"
	StatusDenied   = "denied"
)

// deviceLoginRequestsTable is the table backing this module.
const requestsTable = "public.device_login_requests"

// Errors surfaced to handlers.
var (
	// ErrInvalidRequest covers unknown, expired, or consumed
	// requests and bad device tokens.
	ErrInvalidRequest = errors.New("devicelogin: request is invalid or expired")
	// ErrDenied is returned when the user denied the request.
	ErrDenied = errors.New("devicelogin: request was denied")
	// ErrPending is the typed signal for the long-poll waiter.
	ErrPending = errors.New("devicelogin: authorization pending")
)

// Request is one device login request row.
type Request struct {
	ID              string
	Code            string
	Status          string
	UserID          *string
	DeviceTokenHash string
	IPAddress       string
	UserAgent       string
	CreatedAt       time.Time
	ExpiresAt       time.Time
}

// VerificationInfo is what the approving user sees before deciding.
type VerificationInfo struct {
	UserCode  string    `json:"user_code"`
	Device    string    `json:"device"`
	IPAddress string    `json:"ip_address"`
	City      string    `json:"city"`
	Country   string    `json:"country"`
	ExpiresAt time.Time `json:"expires_at"`
}

// newDeviceToken mints the polling device's secret.
func newDeviceToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashToken hashes a device token for at-rest storage.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// userCode mints a "P" + 7 unambiguous-character code.
func userCode() (string, error) {
	alphabet := "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	out := make([]byte, CodeRandomLen)
	if _, err := rand.Read(out); err != nil {
		return "", err
	}
	for i := range out {
		out[i] = alphabet[int(out[i])%len(alphabet)]
	}
	return CodePrefix + string(out), nil
}

// normalizeCode upper-cases and trims the user code.
func normalizeCode(code string) string {
	return strings.ToUpper(strings.TrimSpace(code))
}
