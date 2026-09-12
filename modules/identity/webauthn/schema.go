// Package webauthn is the primary sign-in method: passkey
// credentials and the begin/finish ceremony endpoints for login and
// re-authentication.
package webauthn

import (
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity"
)

// Typed IDs for the webauthn tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name.
type (
	webauthnCredentialPrefix struct{}

	WebauthnCredentialID = typeid.TypeID[webauthnCredentialPrefix]

	webauthnSessionPrefix struct{}

	WebauthnSessionID = typeid.TypeID[webauthnSessionPrefix]
)

func (webauthnCredentialPrefix) Prefix() string { return "webauthn_credential" }
func (webauthnSessionPrefix) Prefix() string    { return "webauthn_session" }

// Feature is the wireable webauthn unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "webauthn" }

var _ identity.Feature = Feature{}
