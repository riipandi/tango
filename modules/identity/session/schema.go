// Package session is the session subdomain of the identity module:
// token issuance, refresh, validation, and revocation.
//
// Per project convention, subdomain contracts (interfaces + domain
// types) live in schema.go; implementations live in service.go,
// store.go, and handler.go. Dependencies point subpackage -> root
// module contracts, never horizontally between subpackages.
package session
