package scimsync

import "github.com/riipandi/tango/modules/federation"

// Feature is the wireable scimsync unit.
type Feature struct{}

// Name implements federation.Feature.
func (Feature) Name() string { return "scimsync" }

var _ federation.Feature = Feature{}
