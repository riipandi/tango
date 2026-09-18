package apikey

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// currentSelf resolves the caller from the principal the session
// middleware attached to the request context.
func (s *Service) currentSelf(r *http.Request) (string, bool) {
	p, ok := middleware.PrincipalFromContext(r.Context())
	if !ok || p.UserID == "" {
		return "", false
	}
	return p.UserID, true
}

// writeError maps api-key domain errors onto HTTP statuses; anything
// else keeps the shared mapping.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		responder.NotFoundJSON(w, r)
	case errors.Is(err, ErrDuplicate), errors.Is(err, ErrNotExpired), errors.Is(err, ErrInvalidCreds):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	default:
		responder.WriteError(w, r, err)
	}
}

// createKeyRequest is the POST /api-keys payload.
type createKeyRequest struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitzero"`
	ExpiresAt   string  `json:"expires_at"`
}

// renewKeyRequest is the POST /api-keys/{id}/renew payload.
type renewKeyRequest struct {
	ExpiresAt string `json:"expires_at"`
}

// keyResponse is the create/renew wire shape: the key plus the
// one-time token.
type keyResponse struct {
	APIKey APIKey `json:"api_key"`
	Token  string `json:"token"`
}

// APIRoutes mounts the API key endpoints inside the shared /api
// group. The surface is always session-authenticated and scoped to
// the caller; no admin-only routes here.
func (s *Service) APIRoutes(r chi.Router, g identity.RouteGroups) {
	mount := func(ar chi.Router) {
		ar.Get("/api-keys", s.list)
		ar.Post("/api-keys", s.create)
		ar.Delete("/api-keys/{id}", s.revoke)
		ar.Post("/api-keys/{id}/renew", s.renew)
	}

	if g.Self == nil {
		mount(r)
		return
	}
	r.Group(func(ar chi.Router) {
		ar.Use(g.Self)
		mount(ar)
	})
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.currentSelf(r)
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	keys, total, err := s.List(r.Context(), userID, ListParams{Page: Page{Page: params.Page, Limit: params.Limit}})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, keys, responder.WithPaginationFrom(params, total))
}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.currentSelf(r)
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	var req createKeyRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	expiresAt, perr := parseTime(req.ExpiresAt)
	if perr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "invalid expires_at")
		return
	}

	k, token, err := s.Create(r.Context(), userID, CreateParams{
		Name:        req.Name,
		Description: req.Description,
		ExpiresAt:   expiresAt,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, keyResponse{APIKey: k, Token: token})
}

func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.currentSelf(r)
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	id, err := identity.ParseID[APIKeyID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := s.Revoke(r.Context(), userID, id); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Service) renew(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.currentSelf(r)
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	id, err := identity.ParseID[APIKeyID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req renewKeyRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	expiresAt, perr := parseTime(req.ExpiresAt)
	if perr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "invalid expires_at")
		return
	}

	k, token, err := s.Renew(r.Context(), userID, id, RenewParams{ExpiresAt: expiresAt})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, keyResponse{APIKey: k, Token: token})
}

// parseTime parses an RFC 3339 timestamp.
func parseTime(raw string) (time.Time, error) {
	return time.Parse(time.RFC3339, raw)
}
