// Package registry wires feature modules: add = import + one
// Register line, remove = delete the line.
//
// The identity provider surface is isolated in
// modules/federation. To exclude it, delete the
// federation Register line below (plus federation_features.go) — the
// binary keeps all internal authn/authz.
package registry

import (
	"context"
	"fmt"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/antree"
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

	// Queue is the shared task queue client, built by New from
	// DB.Pool(). Features register their queues on it at build time;
	// the queue module runs the dispatcher. Not set by callers.
	Queue *antree.Client
}

// New builds the registry in registration order. Every store is
// Postgres-backed via deps.DB; the adapter routes identity audit
// events into auditlog, keeping the modules decoupled.
func New(deps Deps) *kernel.Registry {
	if deps.DB == nil {
		panic("registry: nil database store")
	}

	reg := kernel.NewRegistry()

	// Task queue: first registered so its Stop drains last. Features
	// register named queues via deps.Queue before the server starts.
	queueClient, err := antree.NewClient(antree.ClientConfig{
		DB:              deps.DB.Pool(),
		Logger:          logger.QueueLogger(deps.Logger),
		NumWorkers:      deps.Config.Queue.Workers,
		ReleaseAfter:    time.Duration(deps.Config.Queue.ReleaseAfter) * time.Second,
		CleanupInterval: time.Duration(deps.Config.Queue.CleanupInterval) * time.Second,
	})
	if err != nil {
		panic(fmt.Sprintf("registry: task queue: %v", err))
	}
	deps.Queue = queueClient
	reg.Register(queue.New(queueClient))

	audit := auditlog.New(auditlog.NewPostgresStore(deps.DB))
	reg.Register(audit)

	// Internal authn/authz: user core + selected auth features.
	reg.Register(identity.New(
		user.NewService(user.NewPostgresStore(deps.DB), auditAdapter(audit)),
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

// auditAdapter converts identity audit events into auditlog entries:
// typed-ID actors/targets map to their UUID columns, anything else
// lands in the payload.
func auditAdapter(audit *auditlog.Module) identity.Recorder {
	return func(ctx context.Context, e identity.AuditEvent) {
		entry := auditlog.Entry{
			Event:   e.Action,
			Trigger: auditlog.TriggerUser,
			Status:  auditlog.StatusSuccess,
			Payload: map[string]any{},
		}

		if actor, err := typeid.Parse[identity.UserID](e.Actor); err == nil {
			uuidText := actor.UUID()
			entry.UserID = &uuidText
		} else {
			entry.Payload["actor"] = e.Actor
		}
		if target, err := typeid.Parse[identity.UserID](e.Target); err == nil {
			uuidText := target.UUID()
			entry.ResourceType = "user"
			entry.ResourceID = &uuidText
		} else {
			entry.Payload["target"] = e.Target
		}

		_ = audit.Record(ctx, &entry)
	}
}
