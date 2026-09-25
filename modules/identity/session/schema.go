package session

import (
	"net/netip"
	"time"

	"go.jetify.com/typeid"
	"uuid"
)

// SessionTable is the table holding the server-side credential rows: today the
// refresh tokens sign-in issues. The migrations own the schema; this constant
// is how Go code names it, so a table rename touches one line.
const SessionTable = "public.sessions"

// SessionPrefix is the TypeID prefix of a session's identifier. A session id
// leaves the server in a token claim and an API response, so the reader of a
// log line or a support ticket can tell what it names without a lookup.
type SessionPrefix struct{}

// Prefix reports the TypeID prefix.
func (SessionPrefix) Prefix() string { return "sess" }

// SessionID is the typed identifier of one row of SessionTable.
type SessionID = typeid.TypeID[SessionPrefix]

// SessionSchema is one row of SessionTable. It lists only the columns the
// application writes, so a migration can add a column with a default without
// touching this struct. The db tags are the column names the query builder
// uses.
type SessionSchema struct {
	ID        SessionID `db:"id"`
	UserID    uuid.UUID `db:"user_id"`
	Provider  string    `db:"provider"`
	TokenHash string    `db:"token_hash"`
	UserAgent string    `db:"user_agent"`
	// DeviceFingerprint is the browser fingerprint the frontend computed,
	// stored beside the session it identifies. The column has no default, so
	// a caller that has none writes an empty string rather than NULL: the
	// distinction carries no meaning here.
	DeviceFingerprint string      `db:"device_fingerprint"`
	IPAddress         *netip.Addr `db:"ip_address"`
	Remember          bool        `db:"remember"`
	CreatedAt         time.Time   `db:"created_at"`
	ExpiresAt         time.Time   `db:"expires_at"`
}
