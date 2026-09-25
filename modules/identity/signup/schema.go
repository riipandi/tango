package signup

import (
	"time"

	"uuid"
)

// SignupTokenTable is the table holding the signup tokens an operator issues.
// The migrations own the schema; this constant is how Go code names it, so a
// table rename touches one line.
const SignupTokenTable = "public.signup_tokens"

// SignupToken is one row of SignupTokenTable, the view a sign-up reads. It
// lists only the columns the feature consumes; the hash is named where its
// query is, never as a struct field.
type SignupToken struct {
	ID         uuid.UUID
	UsageLimit int32
	UsageCount int32
	ExpiresAt  time.Time
}
