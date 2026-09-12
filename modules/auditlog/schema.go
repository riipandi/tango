// Schema contracts and domain types of the auditlog module.
package auditlog

import (
	"context"
	"time"

	"github.com/riipandi/tango/pkg/responder"
	"go.jetify.com/typeid"
)

// AuditLogID identifies an audit_logs row: a UUIDv7 suffix plus a
// snake_case prefix matching the singular table name.
type AuditLogID = typeid.TypeID[auditLogPrefix]

type auditLogPrefix struct{}

func (auditLogPrefix) Prefix() string { return "audit_log" }

// Trigger classifies who emitted the event (audit_event_trigger enum).
type Trigger string

// Trigger values mirroring the database enum.
const (
	TriggerUser     Trigger = "user"
	TriggerSystem   Trigger = "system"
	TriggerExternal Trigger = "external"
)

// Status is the action outcome (audit_action_status enum).
type Status string

// Status values mirroring the database enum.
const (
	StatusSuccess Status = "success"
	StatusFailed  Status = "failed"
	StatusPending Status = "pending"
	StatusUnknown Status = "unknown"
)

// Entry is a single audit record. UserID and ResourceID are raw UUID
// strings (columns), so the module stays decoupled from other
// domains' typed IDs; adapters convert at the boundary.
type Entry struct {
	ID      AuditLogID `json:"id"`
	Event   string     `json:"event"`
	Trigger Trigger    `json:"trigger"`
	Status  Status     `json:"status"`

	Payload map[string]any `json:"payload,omitzero"`

	ResourceType string  `json:"resource_type,omitzero"`
	ResourceID   *string `json:"resource_id,omitzero"`
	UserID       *string `json:"user_id,omitzero"`
	IPAddress    *string `json:"ip_address,omitzero"`
	UserAgent    *string `json:"user_agent,omitzero"`

	CreatedAt time.Time `json:"created_at"`
}

// Store abstracts audit persistence: memory for tests, Postgres for
// production. Record assigns Entry.ID (and CreatedAt when zero) on
// the pointed-to entry. List returns the matching entries (newest
// first) plus the total row count.
type Store interface {
	Record(ctx context.Context, entry *Entry) error
	List(ctx context.Context, params responder.PaginationParams) ([]Entry, int, error)
}
