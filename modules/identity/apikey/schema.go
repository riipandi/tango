// Package apikey is the machine credential subdomain of identity: API
// keys sent via the X-API-KEY header for admin and dashboard use, with
// expiry and last-used tracking. Verification is exposed to transport
// middleware through the identity root's contracts.
//
// Planned files: handler.go (/api/api-keys CRUD), service.go (key
// hashing + verification), store.go.
package apikey

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the apikey subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (users, store) when the subdomain is implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "apikey" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
