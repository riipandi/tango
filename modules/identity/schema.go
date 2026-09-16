// Package identity owns internal authn/authz: user accounts plus the
// selectable features wired at the composition root. The provider
// surface for other systems lives in modules/federation.
package identity

import (
	"context"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/mailer"
)

// NewID generates a UUIDv7-backed typed ID. Panics only on an invalid
// prefix, impossible for the prefixes declared in feature packages.
func NewID[T typeid.Subtype, PT typeid.SubtypePtr[T]]() T {
	return typeid.Must(typeid.New[T, PT]())
}

// ParseID decodes a typed ID string, rejecting prefix mismatches.
func ParseID[T typeid.Subtype, PT typeid.SubtypePtr[T]](s string) (T, error) {
	return typeid.Parse[T, PT](s)
}

// Recorder receives audit events. A nil exec writes the audit row
// standalone (best effort); a non-nil exec joins the caller's
// transaction so the entry commits or rolls back with the domain
// write — the durable outbox boundary.
type Recorder interface {
	Record(ctx context.Context, event AuditEvent, exec datastore.Executor)
}

// MailSender queues transactional email. Implemented by internal/jobs;
// declared here so identity features stay free of the job package.
type MailSender interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

type AuditEvent struct {
	Action string
	Actor  string
	Target string
}
