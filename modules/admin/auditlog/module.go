// Package auditlog captures domain events and exposes them via HTTP.
package auditlog

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/kernel"
)

// ModuleName identifies the audit log module in the registry.
const ModuleName = "auditlog"

// Module is the audit log feature: a Store plus a read-only API.
// The self listing is separate from the admin listing and filters.
type Module struct {
	store Store
	// adminGuard (auth → RequireAdmin) protects /all + filters.
	adminGuard func(http.Handler) http.Handler
	// selfAuth resolves the session credential for the self listing.
	selfAuth kernel.Authenticator
}

// Option configures the audit log module at construction.
type Option func(*Module)

// WithAdminGuard protects the admin listing and filters; without one
// those routes stay unmounted (fail closed).
func WithAdminGuard(guard func(http.Handler) http.Handler) Option {
	return func(m *Module) { m.adminGuard = guard }
}

// WithSelfAuth wires the session resolver for the per-user listing;
// without one that route stays unmounted.
func WithSelfAuth(auth kernel.Authenticator) Option {
	return func(m *Module) { m.selfAuth = auth }
}

// New builds the module on top of store; nil store panics.
func New(store Store, opts ...Option) *Module {
	if store == nil {
		panic("auditlog: nil store")
	}
	m := &Module{store: store}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *Module) Name() string { return ModuleName }

// Record stores an audit entry through the configured store; a
// non-nil exec joins the caller's transaction.
func (m *Module) Record(ctx context.Context, entry *Entry, exec ...datastore.Executor) error {
	return m.store.Record(ctx, entry, exec...)
}
