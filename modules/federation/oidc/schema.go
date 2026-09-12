// Package oidc is the OpenID Connect / OAuth 2.0 provider surface:
// /authorize + PAR (root router), /api/oidc/token, userinfo,
// introspect, revoke, end_session. One flat package, file-per-area;
// split into subpackages only if a coherent boundary emerges.
//
// Files: schema.go (contracts), client.go (relying-party clients),
// token.go (codes, tokens, JWKS), device.go (RFC 8628),
// store_*.go (persistence).
//
// Authentication happens in identity features (passkeys, sessions);
// this package consumes identity contracts via consumer-side adapters.
package oidc

import (
	"github.com/riipandi/tango/modules/federation"
)

// Feature is the wireable oidc unit.
type Feature struct{}

// New returns the placeholder feature. /authorize mounts at the
// root router via the federation RootRoutableFeature capability.
func New() Feature { return Feature{} }

// Name implements federation.Feature.
func (Feature) Name() string { return "oidc" }

var _ federation.Feature = Feature{}
