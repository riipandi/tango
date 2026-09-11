// Package token is the OIDC token subdomain: authorization codes,
// access/refresh tokens, ID token signing keys (JWKS), and token
// lifecycle (introspect, revoke, refresh rotation).
//
// Planned files: schema.go (token + signing-key contracts), service.go
// (minting, verification), store.go. Signs via keys owned here;
// wellknown module exposes the JWKS document.
package token
