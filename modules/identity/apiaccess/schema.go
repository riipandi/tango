// Package apiaccess is the machine authorization subdomain of
// identity: API definitions with scopes/permissions and machine-client
// grants. The oidc module evaluates machine-token access through a
// consumer-side adapter (same pattern as identity.Recorder), so this
// package never imports the oidc module.
//
// Planned files: handler.go (/api/apis CRUD, /api/api-access),
// service.go, store.go.
package apiaccess

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the apiaccess subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (apikeys, clients, store) when the subdomain is
// implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "apiaccess" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
