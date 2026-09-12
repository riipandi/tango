// Schema contracts and domain types of the auditlog module.
package auditlog

import (
	"time"

	"go.jetify.com/typeid"
)

// AuditLogID identifies an audit_logs row: a UUIDv7 suffix plus a
// snake_case prefix matching the singular table name.
type AuditLogID = typeid.TypeID[auditLogPrefix]

type auditLogPrefix struct{}

func (auditLogPrefix) Prefix() string { return "audit_log" }

// Event is a single audit entry, recorded through the module's
// public API instead of touching storage directly.
type Event struct {
	Action    string    `json:"action"`
	Actor     string    `json:"actor"`
	Target    string    `json:"target"`
	Timestamp time.Time `json:"timestamp"`
}
