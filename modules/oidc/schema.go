// Package oidc is the OpenID Connect / OAuth 2.0 provider core: the
// protocol surface of the identity provider. The largest module —
// split by subdomain from day one (see client, token, device
// subpackages).
//
// Root module owns the protocol surface:
//
//	schema.go   — contracts shared across subpackages
//	handler.go  — /authorize + PAR (base group), /api/oidc/token,
//	              userinfo, introspect, revoke, end_session
//	module.go   — kernel wiring: RootRoutable + APIRoutable capabilities
//
// Subpackages: client (clients + CIMD + federated client auth), token
// (codes, access/refresh tokens, signing keys), device (RFC 8628).
// Authentication happens in the identity module (passkeys, sessions);
// this module consumes identity contracts via consumer-side adapters.
package oidc
