// Package registry wires feature modules: add = import + one
// Register line, remove = delete the line.
//
// The identity provider surface is isolated in
// modules/federation. To exclude it, delete the
// federation Register line below (plus federation_features.go) — the
// binary keeps all internal authn/authz.
package registry

import (
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
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

	// Internal authn/authz: user core + selected auth features.
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
		withLDAPSync(deps),
	))

	// Identity provider (OIDC, SCIM, discovery) — optional surface
	// for other systems. Delete this line (and
	// federation_features.go) to exclude the provider entirely.
	reg.Register(federation.New(
		withOIDC(deps),
		withSCIMSync(deps),
		withDiscovery(deps),
	))

	return reg
}
