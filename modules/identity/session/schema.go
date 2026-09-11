// Package session is the session subdomain of the identity module:
// token issuance, refresh, validation, and revocation.
//
// Per project convention, subdomain contracts (interfaces + domain
// types) live in schema.go; implementations live in service.go,
// store.go, and handler.go. Dependencies point subpackage -> root
// module contracts, never horizontally between subpackages.
package session

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the session subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (users, store) when the subdomain is implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "session" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
