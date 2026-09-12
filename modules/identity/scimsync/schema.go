// Package scimsync is the SCIM 2.0 provisioning subdomain of
// identity: create/update/delete users and groups from an external
// identity provider, bearer-guarded under /api/scim/*.
package scimsync

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the scimsync subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (users, groups, store) when implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "scimsync" }

var _ identity.Feature = Feature{}
