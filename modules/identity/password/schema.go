// Package password adds password sign-in alongside passkeys:
// credential storage, login, change, and email-based reset flows.
package password

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable password unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "password" }

var _ identity.Feature = Feature{}
