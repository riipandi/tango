package user

import (
	"errors"
	"net/http"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// createUserRequest is the POST /users payload; optional fields
// default like the store does (empty → NULL / display-name fallback).
type createUserRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	DisplayName string `json:"display_name"`
	IsAdmin     bool   `json:"is_admin"`
}

// APIRoutes mounts the user endpoints inside the shared /api group.
func (s *Service) APIRoutes(r chi.Router) {
	r.Post("/users", s.createUser)
	r.Get("/users", s.listUsers)
	r.Get("/users/{id}", s.getUser)
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := jsonv2.UnmarshalRead(r.Body, &req); err != nil {
		responder.BadRequestJSON(w, r, "invalid request body")
		return
	}

	user, err := s.Create(r.Context(), CreateParams(req))
	if err != nil {
		writeError(w, r, err)
		return
	}

	responder.Success(w, r, http.StatusCreated, user)
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) {
	responder.Success(w, r, http.StatusOK, s.List(r.Context()))
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	user, err := s.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	responder.Success(w, r, http.StatusOK, user)
}

// writeError maps store/service errors to the response envelope:
// validation 400, duplicates 409, missing 404, everything else 500.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrInvalidEmail):
		responder.BadRequestJSON(w, r, err.Error())
	case errors.Is(err, ErrDuplicate):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNotFound):
		responder.NotFoundJSON(w, r)
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
