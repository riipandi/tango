// Package appconfig serves public and admin configuration endpoints.
package appconfig

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// ModuleName identifies the appconfig module in the registry.
const ModuleName = "appconfig"

// Module mounts the configuration endpoints.
type Module struct {
	store       Store
	mailer      MailSender
	guard       func(http.Handler) http.Handler
	envDefaults map[string]string
}

var (
	_ kernel.Module      = (*Module)(nil)
	_ kernel.APIRoutable = (*Module)(nil)
)

// MailSender queues transactional email; internal/jobs implements it.
type MailSender interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

// New builds the module. A nil mailer leaves the test-email route
// unmounted (fail closed); the config CRUD needs WithStore.
func New(mailer MailSender) *Module {
	return &Module{mailer: mailer}
}

// WithStore wires the settings persistence; without it only
// test-email mounts.
func (m *Module) WithStore(store Store) *Module {
	m.store = store
	return m
}

// UseGuard protects the admin routes; without one nothing
// admin-facing mounts.
func (m *Module) UseGuard(guard kernel.Guard) {
	m.guard = guard
}

// WithEnvDefaults seeds the env-backed defaults (LDAP_*/MAILER_*
// values from the koanf config). Catalog defaults stay the bottom
// layer; DB overrides stay the top.
func (m *Module) WithEnvDefaults(defaults map[string]string) *Module {
	m.envDefaults = defaults
	return m
}

// Name implements kernel.Module.
func (*Module) Name() string { return ModuleName }

// APIRoutes mounts the configuration endpoints relative to /api.
func (m *Module) APIRoutes(r chi.Router) {
	// The public bootstrap payload has no guard.
	if m.store != nil {
		r.Get("/application-configuration", m.listPublic)
	}

	if m.store == nil || m.guard == nil {
		if m.guard != nil && m.mailer != nil {
			r.Group(func(admin chi.Router) {
				admin.Use(m.guard)
				admin.Post("/application-configuration/test-email", m.testEmail)
			})
		}
		return
	}
	r.Group(func(admin chi.Router) {
		admin.Use(m.guard)
		admin.Get("/application-configuration/all", m.listAll)
		admin.Put("/application-configuration", m.update)
		if m.mailer != nil {
			admin.Post("/application-configuration/test-email", m.testEmail)
		}
	})
}

// listPublic serves GET /application-configuration: the settings the
// unauthenticated SPA may see (env defaults folded with DB overrides).
func (m *Module) listPublic(w http.ResponseWriter, r *http.Request) {
	overrides, err := m.store.List(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, publicView(m.merged(r.Context(), overrides)))
}

// merged folds the module's env defaults with the stored overrides.
func (m *Module) merged(ctx context.Context, overrides map[string]string) map[string]string {
	return mergedValues(m.envDefaults, overrides)
}

// MergedValues folds env defaults with stored overrides — the
// cross-module read path (the registry wires the CIMD allowlist from
// it).
func (m *Module) MergedValues(ctx context.Context) (map[string]string, error) {
	overrides, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	return m.merged(ctx, overrides), nil
}

// listAll serves GET /application-configuration/all (admin): every
// key with its visibility flag.
func (m *Module) listAll(w http.ResponseWriter, r *http.Request) {
	overrides, err := m.store.List(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, allView(m.merged(r.Context(), overrides)))
}

// updateRequest is the PUT /application-configuration body:
// snake_case settings object. Omitted keys keep their stored value.
type updateRequest map[string]string

func (r updateRequest) Validate() error {
	for key, value := range r {
		entry, known := lookup(key)
		if !known {
			continue // unknown keys are ignored, not rejected
		}
		if err := validateValue(entry, value); err != nil {
			return validation.Errors{key: validation.NewError("validation", err.Error())}
		}
	}
	return nil
}

// update serves PUT /application-configuration (admin): upsert the
// provided keys, then answer with the full view.
func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	var req updateRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	// Only catalog keys persist; the rest never reach the store.
	known := map[string]string{}
	for key, value := range req {
		if _, ok := lookup(key); ok {
			known[key] = value
		}
	}
	if err := m.store.Upsert(r.Context(), known); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	overrides, err := m.store.List(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, allView(m.merged(r.Context(), overrides)))
}

// testEmailRequest is the POST /application-configuration/test-email
// body. An explicit address lets operators test a shared inbox.
type testEmailRequest struct {
	Email string `json:"email,omitzero"`
}

func (r testEmailRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Email, is.EmailFormat),
	)
}

// testEmail queues the test message: 202, because the SMTP
// transaction runs on a worker and the answer cannot report it.
func (m *Module) testEmail(w http.ResponseWriter, r *http.Request) {
	var req testEmailRequest
	if verr := decodeOptional(r, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	// Default to the signed-in administrator.
	to := req.Email
	if to == "" {
		to = principal.Email
	}

	if err := m.mailer.EnqueueEmail(r.Context(), mailer.Message{
		To:       to,
		Subject:  "SMTP test email",
		Template: "test-email",
	}); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to queue email")
		return
	}
	responder.Success(w, r, http.StatusAccepted, map[string]any{
		"queued": true,
		"to":     to,
	})
}

// decodeOptional tolerates a body the caller omitted: every field of
// the test-email request is optional.
func decodeOptional(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return validate.Request(r.Body, dst)
}
