package user

import (
	"errors"
	"net/http"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
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

func (r createUserRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Username, validation.Required, validation.Match(UsernamePattern)),
		validation.Field(&r.Email, validation.Required, is.Email),
	)
}

// updateAdminRequest is the PUT /users/{id} payload; nil fields keep
// the current column value.
type updateAdminRequest struct {
	Email       *string `json:"email,omitzero"`
	FirstName   *string `json:"first_name,omitzero"`
	LastName    *string `json:"last_name,omitzero"`
	DisplayName *string `json:"display_name,omitzero"`
	IsAdmin     *bool   `json:"is_admin,omitzero"`
	Disabled    *bool   `json:"disabled,omitzero"`
}

func (r updateAdminRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Email, is.Email),
		validation.Field(&r.DisplayName, validation.NilOrNotEmpty),
	)
}

// adminParams removed: updateAdminRequest converts directly to
// AdminUpdateParams (identical field sets).

// APIRoutes mounts the user endpoints inside the shared /api group,
// behind the admin guard when one is wired.
func (s *Service) APIRoutes(r chi.Router) {
	mount := func(ar chi.Router) {
		ar.Post("/users", s.createUser)
		ar.Get("/users", s.listUsers)
		ar.Get("/users/{id}", s.getUser)
		ar.Put("/users/{id}", s.updateUser)
		ar.Delete("/users/{id}", s.deleteUser)
	}

	if s.guard == nil {
		mount(r)
		return
	}
	r.Group(func(ar chi.Router) {
		ar.Use(s.guard)
		mount(ar)
	})
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

// listUsers serves the paginated, searchable admin listing.
func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	users, total, err := s.List(r.Context(), ListParams{
		Query:            r.URL.Query().Get("query"),
		PaginationParams: params,
	})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, r, http.StatusOK, users, responder.WithPaginationFrom(params, total))
}

// updateUser patches administrative fields.
func (s *Service) updateUser(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req updateAdminRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	u, err := s.Update(r.Context(), id, AdminUpdateParams(req))
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, u)
}

// deleteUser removes the account; the DB trigger archives it.
func (s *Service) deleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := s.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
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
