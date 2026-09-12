// Package webauthn is the primary sign-in method: passkey
// credentials and the begin/finish ceremony endpoints for login and
// re-authentication.
package webauthn

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable webauthn unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "webauthn" }

var _ identity.Feature = Feature{}
