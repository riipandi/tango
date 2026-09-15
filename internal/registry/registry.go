// Package registry wires feature modules and shared dependencies.
package registry

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.jetify.com/typeid"

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

// Deps are shared dependencies for modules.
type Deps struct {
	// Config is the loaded runtime configuration.
	Config *config.Config

	// Logger is shared by modules.
	Logger logger.Logger

	// Fetcher is the shared outbound client.
	Fetcher *fetcher.Fetcher

	// Mailer is the shared email client.
	Mailer mailer.Mailer

	// DB is the shared Postgres store.
	DB datastore.Store

	// Queue is the shared task queue client built by New.
	Queue *queue.Client

	// Cipher seals secrets at rest.
	Cipher *crypto.Cipher

	// Jobs owns queue registrations and recurring jobs.
	Jobs *jobs.Registry

	// Webhooks emits application events to registered endpoints.
	Webhooks *webhook.Module

	// VersionFeed caches the newest published release.
	VersionFeed *jobs.VersionFeed
}

// New builds the registry in registration order.
func New(deps Deps) *kernel.Registry {
	if deps.DB == nil {
		panic("registry: nil database store")
	}

	reg := kernel.NewRegistry()

	// Register the queue first so it stops last.
	queueClient, err := queue.NewClient(queue.ClientConfig{
		Store:           deps.DB,
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

	// Register email and maintenance consumers.
	deps.Jobs = jobs.NewRegistry(queueClient, deps.Mailer, deps.Logger)
	reg.Register(deps.Jobs)

	audit := auditlog.New(auditlog.NewPostgresStore(deps.DB))
	reg.Register(audit)

	// Domain events are recorded and forwarded to webhooks.
	events := NewEventFanout(deps.Logger)

	// Register identity features.
	ldapSettingsSource := &appconfigRef{}
	core, identityFeatures, adminGuard, sessions, apiAccess, images := newIdentityFeatures(deps, audit, events.Recorder(audit), ldapSettingsSource)
	audit.MountAdminAPI(adminGuard)
	audit.MountSelfAPI(sessions, session.CookieName)
	reg.Register(identity.New(core, identityFeatures...))
	reg.Register(images)

	// Register outbound webhooks.
	webhooks := newWebhookModule(deps, adminGuard)
	deps.Webhooks = webhooks
	reg.Register(webhooks)
	events.Attach(webhooks)

	// Register application configuration.
	appconfigModule := appconfig.New(deps.Jobs).
		WithStore(appconfig.NewPostgresStore(deps.DB)).
		WithEnvDefaults(appconfig.EnvDefaults(deps.Config))
	appconfigModule.UseGuard(adminGuard)
	reg.Register(appconfigModule)
	// Attach appconfig so LDAP sync can read merged settings.
	ldapSettingsSource.Attach(appconfigModule)

	// Register the identity provider surface.
	keyService := newKeyService(deps)
	reg.Register(federation.New(
		withOIDC(deps, audit, keyService, sessions, adminGuard, apiAccess, images.BlobStore(), appconfigModule),
		withSCIMSync(deps),
		keyService,
		withDiscovery(deps, keyService),
	))

	// Register the release feed and its refresh job.
	feed := newVersionFeed(deps)
	deps.VersionFeed = feed
	deps.Jobs.SetVersionFeed(feed)
	registerRecurringJobs(deps, feed, webhooks)

	return reg
}

// auditAdapter converts identity audit events into auditlog entries.
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

// eventFanout forwards recorded events to webhooks.
type eventFanout struct {
	mu   sync.RWMutex
	sink *webhook.Module
	log  logger.Logger
}

// Attach sets the webhook sink.
func (f *eventFanout) Attach(module *webhook.Module) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sink = module
}

// NewEventFanout builds an event fan-out holder.
func NewEventFanout(log logger.Logger) *eventFanout {
	return &eventFanout{log: log}
}

// Recorder records identity events and forwards them to webhooks.
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
