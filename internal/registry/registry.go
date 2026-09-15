// Package registry wires feature modules and shared dependencies.
package registry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"go.jetify.com/typeid"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/fetcher"
	"github.com/riipandi/tango/internal/jobs"
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
	"github.com/riipandi/tango/pkg/responder"
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
}

// Runtime is the concrete composition root: it owns construction
// order, route mounting, and start/stop lifecycle explicitly. Fields
// are filled once by New and never mutated afterwards.
type Runtime struct {
	Queue      *queue.Module
	Jobs       *jobs.Registry
	AuditLog   *auditlog.Module
	Identity   *identity.Module
	Webhook    *webhook.Module
	AppConfig  *appconfig.Module
	Federation *federation.Module
}

// New builds the runtime in registration order.
func New(deps Deps) (*Runtime, error) {
	if deps.DB == nil {
		return nil, errors.New("registry: nil database store")
	}

	rt := &Runtime{}

	// Register the queue first so it stops last.
	queueClient, err := queue.NewClient(queue.ClientConfig{
		Store:           deps.DB,
		Logger:          logger.QueueLogger(deps.Logger),
		NumWorkers:      deps.Config.Queue.Workers,
		ReleaseAfter:    time.Duration(deps.Config.Queue.ReleaseAfter) * time.Second,
		CleanupInterval: time.Duration(deps.Config.Queue.CleanupInterval) * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("registry: task queue: %w", err)
	}
	rt.Queue = queue.New(queueClient)

	// Register email and maintenance consumers.
	rt.Jobs = jobs.NewRegistry(queueClient, deps.Mailer, deps.Logger)

	rt.AuditLog = auditlog.New(auditlog.NewPostgresStore(deps.DB))

	// Domain events are recorded and forwarded to webhooks.
	events := NewEventFanout(deps.Logger)

	// Register identity features.
	core, features, adminGuard, sessions, apiAccess, blobStore, err := newIdentityFeatures(deps, rt.Jobs, rt.AuditLog, events.Recorder(rt.AuditLog))
	if err != nil {
		return nil, err
	}
	rt.AuditLog.MountAdminAPI(adminGuard)
	rt.AuditLog.MountSelfAPI(sessions, session.CookieName)
	rt.Identity = identity.New(core, features...)

	// Register outbound webhooks.
	rt.Webhook = newWebhookModule(deps, queueClient, adminGuard)
	events.Attach(rt.Webhook)

	// Register application configuration.
	rt.AppConfig = appconfig.New(rt.Jobs).
		WithStore(appconfig.NewPostgresStore(deps.DB)).
		WithEnvDefaults(appconfig.EnvDefaults(deps.Config))
	rt.AppConfig.UseGuard(adminGuard)

	// Register the identity provider surface.
	keyService := newKeyService(deps)
	rt.Federation = federation.New(
		withOIDC(deps, rt.AuditLog, keyService, sessions, adminGuard, apiAccess, blobStore, rt.AppConfig),
		withSCIMSync(deps),
		keyService,
		withDiscovery(deps, keyService),
	)

	// Register the release feed and its refresh job.
	feed := newVersionFeed(deps)
	rt.Jobs.SetVersionFeed(feed)
	registerRecurringJobs(deps, rt.Jobs, feed, rt.Webhook)

	return rt, nil
}

// MountRoot mounts root-router routes (OIDC protocol endpoints,
// images) in registration order.
func (rt *Runtime) MountRoot(r chi.Router) {
	rt.Identity.Routes(r)
	rt.Federation.Routes(r)
}

// MountAPI mounts API routes in registration order.
func (rt *Runtime) MountAPI(api chi.Router) {
	api.NotFound(responder.NotFoundJSON)
	api.MethodNotAllowed(responder.MethodNotAllowedJSON)

	rt.AuditLog.APIRoutes(api)
	rt.Identity.APIRoutes(api)
	rt.Webhook.APIRoutes(api)
	rt.AppConfig.APIRoutes(api)
	rt.Federation.APIRoutes(api)
}

// Start starts lifecycle modules in registration order.
func (rt *Runtime) Start(ctx context.Context) error {
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"queue", rt.Queue.Start},
		{"jobs", rt.Jobs.Start},
		{"identity", rt.Identity.Start},
		{"federation", rt.Federation.Start},
	} {
		if err := step.run(ctx); err != nil {
			return fmt.Errorf("start module %q: %w", step.name, err)
		}
	}
	return nil
}

// Stop stops lifecycle modules in reverse order and joins errors.
func (rt *Runtime) Stop(ctx context.Context) error {
	var errs []error
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"federation", rt.Federation.Stop},
		{"identity", rt.Identity.Stop},
		{"jobs", rt.Jobs.Stop},
		{"queue", rt.Queue.Stop},
	} {
		if err := step.run(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop module %q: %w", step.name, err))
		}
	}
	return errors.Join(errs...)
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
