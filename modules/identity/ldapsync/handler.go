package ldapsync

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// SettingsProvider resolves the current LDAP settings (appconfig once
// it is resolved by the caller).
type SettingsProvider func(ctx context.Context) (LDAPSettings, error)

// APIFeature mounts the manual sync endpoint; Startable drives the
// recurring sync.
type APIFeature struct {
	service  *Service
	settings SettingsProvider
	guard    kernel.Guard
}

// New builds the feature over the sync service and a settings source.
func New(service *Service, settings SettingsProvider) *APIFeature {
	return &APIFeature{service: service, settings: settings}
}

// Name implements identity.Feature.
func (*APIFeature) Name() string { return "ldapsync" }

var _ identity.APIFeature = &APIFeature{}

// UseGuard sets the admin middleware for the trigger endpoint.
func (f *APIFeature) UseGuard(g kernel.Guard) {
	f.guard = g
}

// APIRoutes mounts POST /application-configuration/sync-ldap.
// Without a guard nothing mounts — fail closed.
func (f *APIFeature) APIRoutes(r chi.Router) {
	if f.guard == nil {
		return
	}
	r.Group(func(gr chi.Router) {
		gr.Use(f.guard)
		gr.Post("/application-configuration/sync-ldap", f.syncLDAP)
	})
}

// syncLDAP runs one sync inline so the response reports the outcome.
func (f *APIFeature) syncLDAP(w http.ResponseWriter, r *http.Request) {
	settings, err := f.settings(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	stats, err := f.service.SyncAll(r.Context(), settings)
	switch {
	case err == ErrDisabled:
		responder.Fail(w, r, http.StatusConflict, "ldap sync disabled")
		return
	case err == ErrNotConfigured:
		responder.Fail(w, r, http.StatusUnprocessableEntity, "ldap not configured")
		return
	case err != nil:
		responder.Fail(w, r, http.StatusBadGateway, "ldap sync failed",
			responder.WithError(err.Error()))
		return
	}
	responder.Success(w, r, http.StatusOK, stats)
}

// Start arms the recurring sync ticker. It stops on context cancel or
// Stop; a failed run is logged and retried on the next tick (
// semantics: the schedule never dies from one bad sync).
func (f *APIFeature) Start(ctx context.Context) error {
	go f.loop(ctx)
	return nil
}

// Stop implements kernel.Startable; the loop dies with its context.
func (*APIFeature) Stop(context.Context) error { return nil }

func (f *APIFeature) loop(ctx context.Context) {
	ticker := time.NewTicker(SyncInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			settings, err := f.settings(ctx)
			if err != nil {
				continue
			}
			if _, err := f.service.SyncAll(ctx, settings); err != nil && err != ErrDisabled && err != ErrNotConfigured {
				f.service.logger.ErrorContext(ctx, "LDAP sync failed", "error", err)
			}
		}
	}
}
