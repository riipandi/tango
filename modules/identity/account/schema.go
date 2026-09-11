// Package account is the account subdomain of the identity module:
// profile management, credential lifecycle, and account state.
//
// Per project convention, subdomain contracts (interfaces + domain
// types) live in schema.go; implementations live in service.go,
// store.go, and handler.go. Dependencies point subpackage -> root
// module contracts, never horizontally between subpackages.
package account
