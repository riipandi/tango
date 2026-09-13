// Package auditlog captures domain events from other modules and
// exposes them via /api/audit-logs. Storage is abstracted behind
// Store; the composition root picks Postgres (production) — no
// memory store.
package auditlog

import (
	"context"
	"net/http"

	"github.com/riipandi/tango/internal/transport/middleware"
)

// ModuleName identifies the audit log module in the registry.
const ModuleName = "auditlog"

// Module is the audit log feature: a Store plus a read-only API.
// Upstream shape: /audit-logs lists the current user's entries,
// /audit-logs/all and the filter endpoints are admin-only.
type Module struct {
	store Store
	// adminGuard (auth → RequireAdmin) protects /all + filters.
	adminGuard func(http.Handler) http.Handler
	// selfAuth resolves the session cookie for the self listing.
	selfAuth middleware.Authenticator
	cookie   string
}

// New builds the module on top of store; nil store panics.
func New(store Store) *Module {
	if store == nil {
		panic("auditlog: nil store")
	}
	return &Module{store: store}
}

// MountAdminAPI wires the admin guard for the /all + filter routes.
// It must be called before the registry applies routes (the guard
// depends on the session feature built after this module).
func (m *Module) MountAdminAPI(guard func(http.Handler) http.Handler) {
	m.adminGuard = guard
}

// MountSelfAPI wires the session authenticator for the per-user
// listing.
func (m *Module) MountSelfAPI(auth middleware.Authenticator, cookieName string) {
	m.selfAuth = auth
	m.cookie = cookieName
}

func (m *Module) Name() string { return ModuleName }

// Record stores an audit entry through the configured store.
func (m *Module) Record(ctx context.Context, entry *Entry) error {
	return m.store.Record(ctx, entry)
}
