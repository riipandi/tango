package verification

import (
	"time"

	"uuid"
)

// The tables the email-verification feature reads and writes. The migrations
// own the schema; these constants are how Go code names it, so a table rename
// touches one line.

// AuthTokenTable is the one-time-token table the verification row lives in.
const AuthTokenTable = "public.auth_tokens"

// PurposeEmailVerification is the purpose value the verification rows carry.
// The column's check allows it alone among the four; a second purpose here
// would need its own migration first.
const PurposeEmailVerification = "email_verification"

// VerificationToken is one row of AuthTokenTable under the verification
// purpose. The raw value is never stored: the caller's hash is.
type VerificationToken struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	ExpiresAt  time.Time
	LastSentAt *time.Time
}
