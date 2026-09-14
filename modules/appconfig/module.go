// Package appconfig serves the application configuration surface. The
// admin-editable settings CRUD is a later phase; the test-email route
// ships with the phase 7 mail queue, which is what it exercises.
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
	mailer MailSender
	guard  func(http.Handler) http.Handler
}

var (
	_ kernel.Module      = (*Module)(nil)
	_ kernel.APIRoutable = (*Module)(nil)
)

// MailSender queues transactional email; internal/jobs implements it.
type MailSender interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

// New builds the module. A nil mailer leaves the module with no
// routes (fail closed: the endpoint is only meaningful with a queue).
func New(mailer MailSender) *Module {
	return &Module{mailer: mailer}
}

// WithAdminGuard protects the routes; without one nothing mounts.
func (m *Module) WithAdminGuard(guard func(http.Handler) http.Handler) *Module {
	m.guard = guard
	return m
}

// Name implements kernel.Module.
func (*Module) Name() string { return ModuleName }

// APIRoutes mounts the configuration endpoints relative to /api.
func (m *Module) APIRoutes(r chi.Router) {
	if m.guard == nil || m.mailer == nil {
		return
	}
	r.Group(func(admin chi.Router) {
		admin.Use(m.guard)
		admin.Post("/application-configuration/test-email", m.testEmail)
	})
}

// testEmailRequest is the POST /application-configuration/test-email
// body. Upstream addresses the signed-in administrator; an explicit
// address is the tango extension, so an operator can prove delivery to
// a shared inbox.
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

	// Default to the signed-in administrator, upstream parity.
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
