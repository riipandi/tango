// Package webauthn implements passkey sign-in: registration and
// assertion ceremonies via go-webauthn, credential storage, and the
// admin credential CRUD. The challenge state lives in
// webauthn_sessions rows; nothing sensitive is stored raw.
package webauthn

import (
	"errors"
	"time"

	"go.jetify.com/typeid"
)

// Typed IDs: only the URL-facing credential ID carries a TypeID;
// session rows are addressed by their unique challenge.
type (
	credentialPrefix struct{}

	CredentialID = typeid.TypeID[credentialPrefix]
)

func (credentialPrefix) Prefix() string { return "webauthn_credential" }

// Ceremony constants.
const (
	ChallengeRegistration   = "registration"
	ChallengeAuthentication = "authentication"

	credentialsTable = "public.webauthn_credentials"
	sessionsTable    = "public.webauthn_sessions"
)

// Errors surfaced to handlers (mapped to HTTP by the caller).
var (
	// ErrInvalidSession is returned when a ceremony session is
	// unknown, expired, or already consumed.
	ErrInvalidSession = errors.New("webauthn: invalid or expired ceremony session")
	// ErrNotFound is returned when a credential does not exist.
	ErrNotFound = errors.New("webauthn: credential not found")
	// ErrVerificationRequired maps to the missing user-verification
	// flag on an assertion.
	ErrVerificationRequired = errors.New("webauthn: user verification required")
	// ErrAssertionFailed covers assertion/attestation failures.
	ErrAssertionFailed = errors.New("webauthn: ceremony failed")
)

// ChallengeSessionTTL bounds ceremony sessions.
const ChallengeSessionTTL = 60 * time.Second
