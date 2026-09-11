// Package customclaim is the custom-claims subdomain of identity:
// per-user key/value claims injected into OIDC tokens.
//
// Planned files: handler.go (routes under /api/custom-claims),
// service.go, store.go. Consumed by the oidc module when minting
// tokens (via the identity root's exported contracts).
package customclaim
