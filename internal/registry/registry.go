// Package registry wires feature modules and shared dependencies.
package registry

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/riipandi/tango/modules/admin/appconfig"
	"github.com/riipandi/tango/modules/admin/auditlog"
	"github.com/riipandi/tango/modules/federation"
	"github.com/riipandi/tango/modules/identity"
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

	// Route groups shared by the identity and federation surfaces.
	identityGroups   identity.RouteGroups
	federationGroups federation.RouteGroups
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

	// Register email and maintenance consumers; the version feed
	// supplies /api/version/latest.
	feed := newVersionFeed(deps)
	rt.Jobs = jobs.NewRegistry(queueClient, deps.Mailer, deps.Logger, feed)

	// Domain events fan out to audit and webhooks; both sinks are
	// wired below, before the server can serve a request.
	events := NewEventFanout(deps.Logger)
	recorder := events.Recorder()

	// Register identity features: sessions first, then the audit
	// module (its guards need sessions), then the guarded features.
	idModule, groups, sessions, auditLog, apiAccess, blobStore, err := newIdentityFeatures(deps, rt.Jobs, recorder)
	if err != nil {
		return nil, err
	}
	rt.Identity = idModule
	rt.AuditLog = auditLog
	events.audit = rt.AuditLog

	// Register outbound webhooks.
	rt.Webhook = newWebhookModule(deps, queueClient, groups.Admin)
	events.webhook = rt.Webhook

	// Register application configuration.
	rt.AppConfig = appconfig.New(rt.Jobs, appconfig.WithGuard(groups.Admin)).
		WithStore(appconfig.NewPostgresStore(deps.DB)).
		WithEnvDefaults(appconfig.EnvDefaults(deps.Config))

	// Register the identity provider surface.
	keyService := newKeyService(deps)
	rt.Federation = federation.New(
		withOIDC(deps, rt.AuditLog, keyService, sessions, apiAccess, blobStore, rt.AppConfig),
		withSCIMSync(deps),
		keyService,
		withDiscovery(deps, keyService),
	)

	registerRecurringJobs(deps, rt.Jobs, feed, rt.Webhook)

	// The transport boundary mounts these groups; the runtime keeps
	// the chains so every module shares one wiring.
	rt.identityGroups = groups
	rt.federationGroups = federation.RouteGroups{Admin: groups.Admin, Self: groups.Self}

	return rt, nil
}

// MountRoot mounts root-router routes (OIDC protocol endpoints,
// discovery) in registration order.
func (rt *Runtime) MountRoot(r chi.Router) {
	rt.Federation.Routes(r)
}

// MountAPI mounts API routes in registration order.
func (rt *Runtime) MountAPI(api chi.Router) {
	api.NotFound(responder.NotFoundJSON)
	api.MethodNotAllowed(responder.MethodNotAllowedJSON)

	rt.AuditLog.APIRoutes(api)
	rt.Identity.APIRoutes(api, rt.identityGroups)
	rt.Webhook.APIRoutes(api)
	rt.AppConfig.APIRoutes(api)
	rt.Federation.APIRoutes(api, rt.federationGroups)
}

// Start starts lifecycle modules in registration order.
func (rt *Runtime) Start(ctx context.Context) error {
	for _, step := range []struct {
		name string
		run  func(context.Context) error
	}{
		{"queue", rt.Queue.Start},
		{"jobs", rt.Jobs.Start},
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
		{"jobs", rt.Jobs.Stop},
		{"queue", rt.Queue.Stop},
	} {
		if err := step.run(ctx); err != nil {
			errs = append(errs, fmt.Errorf("stop module %q: %w", step.name, err))
		}
	}
	return errors.Join(errs...)
}

// auditAdapter converts identity audit events into auditlog entries
// and joins the caller's transaction when one is provided.
func auditAdapter(audit *auditlog.Module) identity.Recorder {
	return auditRecorder{audit: audit}
}

// auditRecorder adapts auditlog for identity features.
type auditRecorder struct {
	audit *auditlog.Module
}

func (a auditRecorder) Record(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
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

	_ = a.audit.Record(ctx, &entry, exec)
}

// eventFanout forwards recorded events to audit and webhooks. Sinks
// are wired during runtime construction, before any request can fire
// an event.
type eventFanout struct {
	audit   *auditlog.Module
	webhook *webhook.Module
	log     logger.Logger
}

// NewEventFanout builds an event fan-out holder.
func NewEventFanout(log logger.Logger) *eventFanout {
	return &eventFanout{log: log}
}

// Recorder records identity events: one audit entry plus a webhook
// emission per event. The exec joins the audit write to the caller's
// transaction; webhook delivery stays best effort after commit.
// Webhook failure is logged, never fatal.
func (f *eventFanout) Recorder() identity.Recorder {
	return fanoutRecorder{fanout: f}
}

// fanoutRecorder fans one identity event out to audit and webhooks.
type fanoutRecorder struct {
	fanout *eventFanout
}

func (r fanoutRecorder) Record(ctx context.Context, e identity.AuditEvent, exec datastore.Executor) {
	if r.fanout.audit != nil {
		auditAdapter(r.fanout.audit).Record(ctx, e, exec)
	}
	if r.fanout.webhook == nil {
		return
	}
	if err := r.fanout.webhook.Emit(ctx, e.Action, map[string]any{
		"event":  e.Action,
		"actor":  e.Actor,
		"target": e.Target,
	}); err != nil {
		r.fanout.log.WithError(err).Error("webhook fan-out failed for event " + e.Action)
	}
}
