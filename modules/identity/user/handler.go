package user

import (
	"net/http"
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/middleware"
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

// updateProfileRequest is the PUT /users/me payload (self profile;
// account fields mirror UpdateProfileParams).
type updateProfileRequest struct {
	FirstName   *string `json:"first_name,omitzero"`
	LastName    *string `json:"last_name,omitzero"`
	DisplayName *string `json:"display_name,omitzero"`
	AvatarURL   *string `json:"avatar_url,omitzero"`
	Locale      *string `json:"locale,omitzero"`
}

func (r updateProfileRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.DisplayName, validation.NilOrNotEmpty),
	)
}

// adminParams removed: updateAdminRequest converts directly to
// AdminUpdateParams (identical field sets).

// APIRoutes mounts the user endpoints inside the shared /api group.
// Admin CRUD mounts behind the admin guard when one is wired;
// /users/me is self-service (session auth only) and mounts when a
// self authenticator is wired.
func (s *Service) APIRoutes(r chi.Router) {
	if s.selfAuth != nil {
		self := r.With(middleware.RequireAuth(s.selfAuth, s.cookie))
		self.Get("/users/me", s.getCurrentUser)
		self.Put("/users/me", s.updateCurrentUser)
		if s.images != nil {
			self.Put("/users/me/profile-picture", s.updateProfilePicture)
			self.Delete("/users/me/profile-picture", s.resetProfilePicture)
		}
	}

	// Public read: the .png route serves bare bytes with no guard
	// Keep the response body empty.
	if s.images != nil {
		r.Get("/users/{id}/profile-picture.png", s.serveProfilePicture)
	}

	mount := func(ar chi.Router) {
		ar.Post("/users", s.createUser)
		ar.Get("/users", s.listUsers)
		ar.Get("/users/{id}", s.getUser)
		ar.Put("/users/{id}", s.updateUser)
		ar.Delete("/users/{id}", s.deleteUser)
		if s.images != nil {
			ar.Put("/users/{id}/profile-picture", s.updateProfilePicture)
			ar.Delete("/users/{id}/profile-picture", s.resetProfilePicture)
		}
	}

	if s.guard != nil && s.apiGuard != nil {
		// Session-admin and machine (API key) access share one mount:
		// registering the same paths twice makes chi's last
		// registration silently shadow the first.
		r.Group(func(ar chi.Router) {
			ar.Use(eitherGuard(s.guard, s.apiGuard))
			mount(ar)
		})
	} else if s.guard != nil {
		r.Group(func(ar chi.Router) {
			ar.Use(s.guard)
			mount(ar)
		})
	} else if s.apiGuard != nil {
		r.Group(func(ar chi.Router) {
			ar.Use(s.apiGuard)
			mount(ar)
		})
	} else {
		mount(r)
	}
}

// eitherGuard prefers the session-admin path; requests carrying an
// X-API-KEY header go to the machine guard instead.
func eitherGuard(admin, api kernel.Guard) kernel.Guard {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.TrimSpace(r.Header.Get("X-API-KEY")) != "" {
				api(next).ServeHTTP(w, r)
				return
			}
			admin(next).ServeHTTP(w, r)
		})
	}
}

// getCurrentUser serves GET /users/me: the signed-in user's own
// record used by the SPA.
func (s *Service) getCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := identity.ParseID[UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	u, err := s.GetByID(r.Context(), id)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, u)
}

// updateCurrentUser serves PUT /users/me: profile self-service —
// updates the profile; email changes require admin access.
func (s *Service) updateCurrentUser(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	id, err := identity.ParseID[UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	var req updateProfileRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	u, err := s.store.UpdateProfile(r.Context(), id, UpdateProfileParams(req))
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, u)
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := jsonv2.UnmarshalRead(r.Body, &req); err != nil {
		responder.BadRequestJSON(w, r, "invalid request body")
		return
	}

	user, err := s.Create(r.Context(), CreateParams(req))
	if err != nil {
		responder.WriteError(w, r, err)
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
		responder.WriteError(w, r, err)
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
		responder.WriteError(w, r, err)
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
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}
