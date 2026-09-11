// Package registry wires the application's feature modules. This is
// the composition point of the monolith: adding a module = import +
// one Register line, removing one = delete the line.
package registry

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/wellknown"
)

// Deps carries the shared dependencies every module may draw from.
// Modules never read them from package globals; constructors receive
// only what they need.
type Deps struct {
	// Config is the loaded runtime configuration.
	Config *config.Config

	// Logger is the shared application logger. Modules receive it
	// to emit structured entries; they never build their own.
	Logger logger.Logger

	// Fetcher is the shared outbound HTTP client for service
	// integrations. Built once and reused for its connection pool.
	Fetcher *fetcher.Fetcher

	// Mailer renders and delivers transactional email from the
	// embedded React Email templates.
	Mailer mailer.Mailer

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
	reg.Register(identity.New(
		// Mandatory user core.
		user.NewService(user.NewMemoryStore(), func(e identity.AuditEvent) {
			audit.Record(auditlog.Event{Action: e.Action, Actor: e.Actor, Target: e.Target})
		}),
		// Identity authn/authz feature selection — add or remove a line to change the feature set.
		withSession(deps),
		withWebAuthn(deps),
		withPassword(deps),
		withAPIKeys(deps),
		withAPIAccess(deps),
		withOIDC(deps),
	))

	return reg
}
