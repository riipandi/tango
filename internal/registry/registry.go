// Package registry wires feature modules: add = import + one
// Register line, remove = delete the line.
package registry

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/wellknown"
)

// Deps are shared dependencies for modules. No globals;
// constructors take what they need.
type Deps struct {
	// Config is the loaded runtime configuration.
	Config *config.Config

	// Logger is shared; modules never build their own.
	Logger logger.Logger

	// Fetcher is the shared outbound client (pooled).
	Fetcher *fetcher.Fetcher

	// Mailer is the shared email client.
	Mailer mailer.Mailer

	// DB is the shared Postgres store. Modules get stores built
	// on it, never the pool itself.
	DB datastore.Store
}

// New builds the registry in registration order. The adapter routes
// identity audit events into auditlog, keeping them decoupled.
func New(deps Deps) *kernel.Registry {
	reg := kernel.NewRegistry()

	audit := auditlog.New()
	reg.Register(audit)
	reg.Register(wellknown.New())
	reg.Register(identity.New(
		user.NewService(user.NewMemoryStore(), func(e identity.AuditEvent) {
			audit.Record(auditlog.Event{Action: e.Action, Actor: e.Actor, Target: e.Target})
		}),
		// Feature selection: add/remove a line to change the set.
		withSession(deps),
		withWebAuthn(deps),
		withPassword(deps),
		withAPIKeys(deps),
		withAPIAccess(deps),
		withOIDC(deps),
		withLDAPSync(deps),
		withSCIMSync(deps),
	))

	return reg
}
