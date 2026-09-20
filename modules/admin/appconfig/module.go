// Package appconfig serves public and admin configuration endpoints.
package appconfig

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/pkg/responder"
)

// ModuleName identifies the appconfig module in the registry.
const ModuleName = "appconfig"

// Module mounts the configuration endpoints.
type Module struct {
	store  Store
	mailer MailSender
}

// MailSender queues transactional email; internal/jobs implements it.
type MailSender interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

// New builds the module. A nil mailer leaves the test-email route
// unmounted (fail closed); the config CRUD needs WithStore.
func New(mailer MailSender, opts ...Option) *Module {
	m := &Module{mailer: mailer}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithStore wires the settings persistence; without it only
// test-email mounts.
func (m *Module) WithStore(store Store) *Module {
	m.store = store
	return m
}

// Option configures the appconfig module at construction.
type Option func(*Module)

// Name identifies the module.
func (*Module) Name() string { return ModuleName }

// APIRoutes mounts the retained public bootstrap endpoint relative
// to /api. The admin CRUD and test-email surfaces serve ConnectRPC
// exclusively (see handler_rpc.go).
func (m *Module) APIRoutes(r chi.Router) {
	if m.store != nil {
		r.Get("/application-configuration", m.listPublic)
	}
}

// listPublic serves GET /application-configuration: the settings the
// unauthenticated SPA may see (catalog defaults folded with DB
// overrides).
func (m *Module) listPublic(w http.ResponseWriter, r *http.Request) {
	overrides, err := m.store.List(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, publicView(mergedValues(overrides)))
}

// MergedValues folds the catalog defaults with the stored overrides —
// the cross-module read path (the registry wires the CIMD allowlist
// from it).
func (m *Module) MergedValues(ctx context.Context) (map[string]string, error) {
	overrides, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return mergedValues(overrides), nil
}
