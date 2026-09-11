// Schema contracts and domain types of the identity module:
// interfaces live here so alternate implementations (memory, Postgres)
// stay swappable without touching handlers or business rules.
package identity

import (
	"context"
	"errors"
)

// User is the identity module's core entity.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Store abstracts user persistence. Swap in a database-backed
// implementation without touching handlers or business rules.
type Store interface {
	List(ctx context.Context) []User
	Create(ctx context.Context, name string) (User, error)
	GetByID(ctx context.Context, id string) (User, bool)
}

// Recorder receives audit events from identity. It is a function type
// so the composition root can adapt any sink (e.g. the auditlog
// module) without identity importing that module.
type Recorder func(event AuditEvent)

// AuditEvent is what identity emits to a recorder. Declared here (not
// imported from auditlog) so modules stay decoupled.
type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// ErrInvalidName is returned when a user payload fails validation.
var ErrInvalidName = errors.New("name is required")
