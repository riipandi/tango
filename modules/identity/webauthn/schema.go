// Package webauthn is the passkey authentication subdomain of
// identity: WebAuthn credentials and the begin/finish ceremony
// endpoints for login and re-authentication. This is the primary
// authentication method — sign-in is passkeys-only.
//
// Planned files:
//
//	schema.go   — Passkey, WebAuthnSession contracts + credential store
//	handler.go  — /api/webauthn/* (register, login, reauthenticate begin/finish)
//	service.go  — ceremony orchestration over go-webauthn
//	store.go    — credential/session persistence (file-per-backend)
package webauthn

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the webauthn subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (users, sessions, store) when the subdomain is
// implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "webauthn" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
