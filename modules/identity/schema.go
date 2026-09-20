// Package identity owns internal authn/authz: user accounts plus the
// selectable features wired at the composition root. The provider
// surface for other systems lives in modules/federation.
package identity

import (
	"context"
	"time"

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

// PendingCookieName is the browser cookie carrying the pending-auth
// token; the raw value lives only in the cookie.
const PendingCookieName = "tango_mfa_pending"

// MFAPendingIssuer is the identity-owned second-factor port between
// sign-in and full session issuance: a confirmed enrollment turns a
// successful password sign-in into a pending authentication that
// only the MFA verification endpoint can upgrade. The user ID is the
// TypeID wire form.
type MFAPendingIssuer interface {
	// RequiresPending reports whether the account must pass the
	// second factor before a full session.
	RequiresPending(ctx context.Context, userID string) (bool, error)
	// CreatePending mints the short-lived pending bridge and returns
	// its raw token (for the pending cookie only). remember carries
	// the sign-in duration request across the second factor, so the
	// session issued after verification keeps the requested lifetime.
	CreatePending(ctx context.Context, userID string, remember bool) (string, error)
	// ClearPending drops the bridge (sign-out).
	ClearPending(ctx context.Context, userID string) error
}

// PendingCookieTTL bounds the pending-auth cookie and row.
const PendingCookieTTL = 5 * time.Minute

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
