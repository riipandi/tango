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
	"sync"
	"time"

	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/antree"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/queue"
	"github.com/riipandi/tango/modules/appconfig"
	"github.com/riipandi/tango/modules/auditlog"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/webhook"
	"github.com/riipandi/tango/pkg/crypto"
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

	// Cipher seals secrets at rest (webhook signing secrets). Built
	// by New from auth.secret_key; not set by callers.
	Cipher *crypto.Cipher

	// Jobs owns the queue registrations and recurring jobs. Built by
	// New; not set by callers.
	Jobs *jobs.Registry

	// Webhooks emits application events to registered endpoints.
	// Built by New; not set by callers.
	Webhooks *webhook.Module

	// VersionFeed caches the newest published release for
	// /api/version/latest. Built by New; not set by callers.
	VersionFeed *jobs.VersionFeed
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

	// Queue consumers: transactional email plus the recurring
	// maintenance jobs. Registered right after the dispatcher adapter
	// so their types exist before any producer enqueues.
	deps.Jobs = jobs.NewRegistry(queueClient, deps.Mailer, deps.Logger)
	reg.Register(deps.Jobs)

	audit := auditlog.New(auditlog.NewPostgresStore(deps.DB))
	reg.Register(audit)

	// Domain events fan out to webhooks on top of the audit row: the
	// recorder is the single funnel identity features already use.
	events := NewEventFanout(deps.Logger)

	// Internal authn/authz: user core + selected auth features.
	ldapSettingsSource := &appconfigRef{}
	core, identityFeatures, adminGuard, sessions, apiAccess, images := newIdentityFeatures(deps, audit, events.Recorder(audit), ldapSettingsSource)
	audit.MountAdminAPI(adminGuard)
	audit.MountSelfAPI(sessions, session.CookieName)
	reg.Register(identity.New(core, identityFeatures...))
	reg.Register(images)

	// Outbound webhooks: an admin-managed surface, dispatched by the
	// queue. Not upstream — a tango extension.
	webhooks := newWebhookModule(deps, adminGuard)
	deps.Webhooks = webhooks
	reg.Register(webhooks)
	events.Attach(webhooks)

	// Application configuration: settings CRUD (phase 9B) plus the
	// test-email slice riding the phase 7 mail queue.
	appconfigModule := appconfig.New(deps.Jobs).
		WithStore(appconfig.NewPostgresStore(deps.DB)).
		WithEnvDefaults(appConfigEnvDefaults(deps.Config))
	appconfigModule.UseGuard(adminGuard)
	reg.Register(appconfigModule)
	// LDAP sync reads its settings through the appconfig surface; the
	// ref fills in now that the module exists (identity built first).
	ldapSettingsSource.Attach(appconfigModule)

	// Identity provider (OIDC, SCIM, discovery) — optional surface
	// for other systems. Delete this line (and
	// federation_features.go) to exclude the provider entirely.
	keyService := newKeyService(deps)
	reg.Register(federation.New(
		withOIDC(deps, audit, keyService, sessions, adminGuard, apiAccess, images.BlobStore(), appconfigModule),
		withSCIMSync(deps),
		keyService,
		withDiscovery(deps, keyService),
	))

	// Latest-release feed: a recurring job fills the cache that
	// /api/version/latest reads.
	feed := newVersionFeed(deps)
	deps.VersionFeed = feed
	deps.Jobs.SetVersionFeed(feed)
	registerRecurringJobs(deps, feed, webhooks)

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

		if actor, err := typeid.Parse[user.UserID](e.Actor); err == nil {
			uuidText := actor.UUID()
			entry.UserID = &uuidText
		} else {
			entry.Payload["actor"] = e.Actor
		}
		if target, err := typeid.Parse[user.UserID](e.Target); err == nil {
			uuidText := target.UUID()
			entry.ResourceType = "user"
			entry.ResourceID = &uuidText
		} else {
			entry.Payload["target"] = e.Target
		}

		_ = audit.Record(ctx, &entry)
	}
}

// eventFanout forwards application events to registered webhooks. The
// sink is attached after the webhook module is built (identity
// features need the recorder first, and the module needs the admin
// guard those features produce), so the indirection breaks the cycle
// without a global.
type eventFanout struct {
	mu   sync.RWMutex
	sink *webhook.Module
	log  logger.Logger
}

// Attach wires the sink; the recorder starts fanning out afterwards.
func (f *eventFanout) Attach(module *webhook.Module) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sink = module
}

// NewEventFanout builds the fan-out holder with the shared logger.
func NewEventFanout(log logger.Logger) *eventFanout {
	return &eventFanout{log: log}
}

// Recorder is the identity audit recorder that additionally fans the
// event out to subscribed webhooks. Delivery never blocks or fails the
// request: the outbox write happens on its own transaction and a
// fan-out error is logged, not surfaced.
func (f *eventFanout) Recorder(audit *auditlog.Module) identity.Recorder {
	return func(ctx context.Context, e identity.AuditEvent) {
		auditAdapter(audit)(ctx, e)

		f.mu.RLock()
		sink := f.sink
		f.mu.RUnlock()
		if sink == nil {
			return
		}
		if err := sink.Emit(ctx, e.Action, map[string]any{
			"event":  e.Action,
			"actor":  e.Actor,
			"target": e.Target,
		}); err != nil {
			f.log.WithError(err).Error("webhook fan-out failed for event " + e.Action)
		}
	}
}
