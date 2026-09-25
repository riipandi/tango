package signup

import (
	"time"

	"uuid"
)

// SignupTokenTable is the table holding the signup tokens an operator issues.
// The migrations own the schema; this constant is how Go code names it, so a
// table rename touches one line.
const SignupTokenTable = "public.signup_tokens"

// SignupToken is one row of SignupTokenTable, the view the feature reads and
// the operators manage. The raw token is not a field: only its hash is stored.
type SignupToken struct {
	ID         uuid.UUID
	UsageLimit int32
	UsageCount int32
	CreatedAt  time.Time
	ExpiresAt  time.Time
}
