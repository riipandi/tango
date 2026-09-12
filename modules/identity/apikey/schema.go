// Package apikey issues machine credentials sent via the X-API-KEY
// header, with expiry and last-used tracking.
package apikey

import (
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/modules/identity"
)

// Typed IDs for the api key tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	apiKeyPrefix struct{}

	APIKeyID = typeid.TypeID[apiKeyPrefix]
)

func (apiKeyPrefix) Prefix() string { return "api_key" }

// Feature is the wireable apikey unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "apikey" }

var _ identity.Feature = Feature{}
