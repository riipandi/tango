// Package multifactor owns second-factor methods beyond passkeys:
// TOTP keys and recovery codes.
package multifactor

import "go.jetify.com/typeid"

// Typed IDs for the MFA tables: UUIDv7 suffix, snake_case prefix
// matching the singular table name.
type (
	mfaKeyPrefix struct{}

	MFAKeyID = typeid.TypeID[mfaKeyPrefix]
)

func (mfaKeyPrefix) Prefix() string { return "mfa_key" }
