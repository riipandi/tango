// Package scimsync is the SCIM 2.0 provisioning surface: create/
// update/delete users and groups from an external identity provider,
// bearer-guarded under /api/scim/*.
package scimsync

import (
	"github.com/riipandi/tango/modules/federation"
)

// Feature is the wireable scimsync unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements federation.Feature.
func (Feature) Name() string { return "scimsync" }

var _ federation.Feature = Feature{}
