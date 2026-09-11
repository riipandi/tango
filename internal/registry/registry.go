// Package registry wires the application's feature modules. This is
// the composition point of the monolith: adding a module = import +
// one Register line, removing one = delete the line.
package registry

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/wellknown"
)

// Deps carries the shared dependencies every module may draw from.
// Modules never read them from package globals; constructors receive
// only what they need.
type Deps struct {
	// Config is the loaded runtime configuration.
	Config *config.Config

	// DB is the shared database pool. Populated once Postgres
	// support lands; modules receive store implementations built
	// on top of it, never the pool itself.
	// DB *pgxpool.Pool

	// Mailer is the shared email client for modules that send mail.
	// Mailer mailer.Mailer
}

// New builds the module registry with every active module, in
// registration order. The adapter below routes identity audit events
// into the auditlog module, keeping the two decoupled.
func New(deps Deps) *kernel.Registry {
	reg := kernel.NewRegistry()

	audit := auditlog.New()
	reg.Register(audit)
	reg.Register(wellknown.New())
	reg.Register(identity.New(identity.NewMemoryStore(), func(e identity.AuditEvent) {
		audit.Record(auditlog.Event{Action: e.Action, Actor: e.Actor, Target: e.Target})
	}))

	return reg
}
