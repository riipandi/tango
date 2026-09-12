// Package apiaccess manages machine authorization: API definitions
// with scopes and client grants. The federation module evaluates
// machine-token access via a consumer-side adapter, so this package
// never imports it.
package apiaccess

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable apiaccess unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "apiaccess" }

var _ identity.Feature = Feature{}
