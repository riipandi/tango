// Package apikey issues machine credentials sent via the X-API-KEY
// header, with expiry and last-used tracking.
package apikey

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable apikey unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "apikey" }

var _ identity.Feature = Feature{}
