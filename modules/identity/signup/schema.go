// Package signup implements application-based self-service sign-up
// flows (create + approve) with rate-limited endpoints.
package signup

import "go.jetify.com/typeid"

// Typed IDs for the signup and invitation tables: UUIDv7 suffix,
// snake_case prefix matching the singular table name.
type (
	signupTokenPrefix struct{}

	SignupTokenID = typeid.TypeID[signupTokenPrefix]

	invitationPrefix struct{}

	InvitationID = typeid.TypeID[invitationPrefix]
)

func (signupTokenPrefix) Prefix() string { return "signup_token" }
func (invitationPrefix) Prefix() string  { return "invitation" }
