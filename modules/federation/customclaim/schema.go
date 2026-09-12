// Package customclaim injects per-user key/value claims into OIDC
// tokens; consumed by the oidc package via the federation root's
// contracts.
package customclaim

import "go.jetify.com/typeid"

// Typed IDs for the custom claim tables: UUIDv7 suffix, snake_case
// prefix matching the singular table name.
type (
	customClaimPrefix struct{}

	CustomClaimID = typeid.TypeID[customClaimPrefix]
)

func (customClaimPrefix) Prefix() string { return "custom_claim" }
