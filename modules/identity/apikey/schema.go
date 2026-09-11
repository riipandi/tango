// Package apikey is the machine credential subdomain of identity: API
// keys sent via the X-API-KEY header for admin and dashboard use, with
// expiry and last-used tracking. Verification is exposed to transport
// middleware through the identity root's contracts.
//
// Planned files: handler.go (/api/api-keys CRUD), service.go (key
// hashing + verification), store.go.
package apikey
