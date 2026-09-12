// Package scimsync is the SCIM 2.0 provisioning surface: create/
// update/delete users and groups from an external identity provider,
// bearer-guarded under /api/scim/*.
package scimsync

import (
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/federation"
)

// Typed IDs for the SCIM tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	scimServiceProviderPrefix struct{}

	SCIMServiceProviderID = typeid.TypeID[scimServiceProviderPrefix]
)

func (scimServiceProviderPrefix) Prefix() string { return "scim_service_provider" }

// Feature is the wireable scimsync unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements federation.Feature.
func (Feature) Name() string { return "scimsync" }

var _ federation.Feature = Feature{}
