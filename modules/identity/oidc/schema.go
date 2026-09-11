// Package oidc is the OpenID Connect / OAuth 2.0 provider subdomain
// of the identity module: the protocol surface of the identity
// provider. One flat package, split by file — promoted to its own
// subpackage only if a coherent boundary emerges as it grows.
//
// Files:
//
//	schema.go  — contracts shared inside the subdomain (this file)
//	handler.go — /authorize + PAR (root router), /api/oidc/token,
//	             userinfo, introspect, revoke, end_session
//	client.go  — relying-party clients CRUD, callback wildcards,
//	             CIMD (Client-ID Metadata Documents), federated
//	             client auth
//	token.go   — authorization codes, access/refresh tokens, ID
//	             token signing keys (JWKS), introspection/revocation
//	device.go  — RFC 8628 device authorization grant
//	store_*.go — persistence, file-per-backend
//
// Authentication happens in sibling identity features (passkeys,
// sessions); this subdomain consumes identity contracts via
// consumer-side adapters.
package oidc

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the oidc subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (users, sessions, signing keys, store) when the
// subdomain is implemented; /authorize will mount at the root router
// via the identity RootRoutableFeature capability.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "oidc" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
