// Package registry wires the application's feature modules. This is
// the composition point of the monolith: adding a module = import +
// one Register line, removing one = delete the line.
package registry

import (
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/wellknown"
)

// New builds the module registry with every active module, in
// registration order. The adapter below routes identity audit events
// into the auditlog module, keeping the two decoupled.
func New() *kernel.Registry {
	reg := kernel.NewRegistry()

	audit := auditlog.New()
	reg.Register(audit)
	reg.Register(wellknown.New())
	reg.Register(identity.New(identity.NewMemoryStore(), func(e identity.AuditEvent) {
		audit.Record(auditlog.Event{Action: e.Action, Actor: e.Actor, Target: e.Target})
	}))

	return reg
}
