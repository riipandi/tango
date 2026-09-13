// Package customclaim injects per-user and per-group claims into
// OIDC tokens. Claim ownership and CRUD live in the identity side
// (modules/identity/customclaim) — this package only reads them via
// a consumer-side adapter at token time.
package customclaim
