package webhook

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// ModuleName identifies the webhook module in the registry.
const ModuleName = "webhook"

// Module is the webhook feature: endpoint CRUD, delivery logs, and the
// outbox event sink. Routes stay behind the admin guard; without one
// nothing mounts (fail closed).
type Module struct {
	service *Service

	adminGuard func(http.Handler) http.Handler
}

var (
	_ kernel.Module      = (*Module)(nil)
	_ kernel.APIRoutable = (*Module)(nil)
)

// Option configures the webhook module at construction.
type Option func(*Module)

// New builds the module on top of the service. Options wire the
// admin guard; without one nothing mounts (fail closed).
func New(service *Service, opts ...Option) *Module {
	if service == nil {
		panic("webhook: nil service")
	}
	m := &Module{service: service}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// WithGuard protects every route. Session auth wrapped in
// RequireAdmin is the production wiring.
func WithGuard(guard kernel.Guard) Option {
	return func(m *Module) { m.adminGuard = guard }
}

// Store exposes the persistence layer for the recurring log-pruning
// job; the module itself never needs a wider surface.
func (m *Module) Store() Store { return m.service.store }

// Emit fans an application event out to its subscribers. It is the
// event sink the composition root hands to other modules, so a domain
// package never imports this one directly.
func (m *Module) Emit(ctx context.Context, event string, payload map[string]any) error {
	return m.service.Emit(ctx, event, payload)
}

// Name implements kernel.Module.
func (*Module) Name() string { return ModuleName }

// APIRoutes mounts the endpoints relative to the /api group.
func (m *Module) APIRoutes(r chi.Router) {
	if m.adminGuard == nil {
		return // fail closed: webhooks are administrative
	}

	r.Group(func(admin chi.Router) {
		admin.Use(m.adminGuard)
		admin.Get("/webhooks", m.list)
		admin.Post("/webhooks", m.create)
		admin.Get("/webhooks/{id}", m.get)
		admin.Put("/webhooks/{id}", m.update)
		admin.Delete("/webhooks/{id}", m.remove)
		admin.Post("/webhooks/{id}/rotate-secret", m.rotateSecret)
		admin.Post("/webhooks/{id}/test", m.test)
		admin.Get("/webhooks/{id}/logs", m.listLogs)
		admin.Get("/webhook-logs", m.listAllLogs)
	})
}

// list serves GET /webhooks.
func (m *Module) list(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	listParams := ListParams{PaginationParams: params}
	if raw := r.URL.Query().Get("enabled"); raw != "" {
		enabled, parseErr := strconv.ParseBool(raw)
		if parseErr != nil {
			responder.Fail(w, r, http.StatusUnprocessableEntity, "invalid enabled filter")
			return
		}
		listParams.Enabled = &enabled
	}
	listParams.Event = strings.TrimSpace(r.URL.Query().Get("event"))

	hooks, total, err := m.service.List(r.Context(), listParams)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, hooks, responder.WithPaginationFrom(params, total))
}

func (m *Module) create(w http.ResponseWriter, r *http.Request) {
	var req CreateParams
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	hook, err := m.service.Create(r.Context(), req)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, hook)
}

func (m *Module) get(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	hook, err := m.service.Get(r.Context(), id)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, hook)
}

func (m *Module) update(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	var req UpdateParams
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	hook, err := m.service.Update(r.Context(), id, req)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, hook)
}

func (m *Module) remove(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := m.service.Delete(r.Context(), id); err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (m *Module) rotateSecret(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	secret, err := m.service.RotateSecret(r.Context(), id)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]string{
		"id":     id.String(),
		"secret": secret,
	})
}

// testRequest is the POST /webhooks/{id}/test body.
type testRequest struct {
	Event   string         `json:"event,omitzero"`
	Payload map[string]any `json:"payload,omitzero"`
}

// defaultTestEvent labels the synthetic delivery used by the test
// route; subscribers of "*" or this name receive it.
const defaultTestEvent = "webhook.test"

// test emits a synthetic event so an operator can verify a receiver
// without waiting for real traffic. The response reports the log ID
// so the delivery can be followed in the log listing.
func (m *Module) test(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	var req testRequest
	if verr := decodeOptionalBody(r, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	// The endpoint is addressed directly rather than through the
	// subscription filter: a test must reach a receiver even when its
	// subscription list does not name the synthetic event.
	logID, err := m.service.DeliverTo(r.Context(), id, defaultTestEvent, map[string]any{
		"event":  defaultTestEvent,
		"source": "tango",
	})
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusAccepted, map[string]any{
		"log_id": logID.String(),
		"event":  defaultTestEvent,
	})
}

func (m *Module) listLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := parseIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}
	m.writeLogs(w, r, &id)
}

func (m *Module) listAllLogs(w http.ResponseWriter, r *http.Request) {
	m.writeLogs(w, r, nil)
}

func (m *Module) writeLogs(w http.ResponseWriter, r *http.Request, id *WebhookID) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	logs, total, err := m.service.ListLogs(r.Context(), id, PageParams(params))
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, logs, responder.WithPaginationFrom(params, total))
}

// parseIDParam resolves the {id} path segment into a typed ID.
func parseIDParam(r *http.Request) (WebhookID, bool) {
	id, err := parseWebhookID(chi.URLParam(r, "id"))
	if err != nil {
		return WebhookID{}, false
	}
	return id, true
}

// decodeOptionalBody decodes a request body that a caller may omit.
// validate.Request treats an empty body as malformed JSON, which is
// wrong for a route whose fields are all optional: the operator sends
// no body at all to trigger the default event.
func decodeOptionalBody(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return validate.Request(r.Body, dst)
}
