// Package session manages sign-in sessions: issue, refresh,
// validate, and revoke.
package session

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable session unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "session" }

var _ identity.Feature = Feature{}
