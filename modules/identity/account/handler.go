package account

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"
	"github.com/go-ozzo/ozzo-validation/v4/is"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// updateProfileRequest patches profile fields; nil keeps the column.
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
		validation.Field(&r.AvatarURL, is.URL),
	)
}

// changePasswordRequest is the PUT /account/password payload.
type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (r changePasswordRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.CurrentPassword, validation.Required),
		validation.Field(&r.NewPassword, validation.Required, validation.Length(8, 0)),
	)
}

// sessionView is the client-safe session projection: never the
// token hash.
type sessionView struct {
	ID          string     `json:"id"`
	Provider    string     `json:"provider"`
	UserAgent   *string    `json:"user_agent,omitzero"`
	DeviceName  *string    `json:"device_name,omitzero"`
	IPAddress   *string    `json:"ip_address,omitzero"`
	CreatedAt   time.Time  `json:"created_at"`
	ExpiresAt   time.Time  `json:"expires_at"`
	RefreshedAt *time.Time `json:"refreshed_at,omitzero"`
}

// APIRoutes mounts self-service endpoints, all cookie-guarded.
func (s *Service) APIRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(s.sessions, session.CookieName))
		r.Get("/account", s.profile)
		r.Patch("/account", s.updateProfile)
		r.Put("/account/password", s.changePasswordHTTP)
		r.Get("/account/sessions", s.listSessions)
		r.Delete("/account/sessions/{id}", s.revokeSession)
	})
}

// principalUserID resolves the guarded caller to a typed ID.
func principalUserID(r *http.Request) (user.UserID, error) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		return user.UserID{}, errors.New("account: unauthenticated")
	}
	return identity.ParseID[user.UserID](principal.UserID)
}

func (s *Service) profile(w http.ResponseWriter, r *http.Request) {
	id, err := principalUserID(r)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	u, err := s.users.GetByID(r.Context(), id)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	responder.Success(w, r, http.StatusOK, u)
}

func (s *Service) updateProfile(w http.ResponseWriter, r *http.Request) {
	id, err := principalUserID(r)
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

	u, err := s.users.UpdateProfile(r.Context(), id, user.UpdateProfileParams{
		FirstName:   req.FirstName,
		LastName:    req.LastName,
		DisplayName: req.DisplayName,
		AvatarURL:   req.AvatarURL,
		Locale:      req.Locale,
	})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, u)
}

func (s *Service) changePasswordHTTP(w http.ResponseWriter, r *http.Request) {
	id, err := principalUserID(r)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	var req changePasswordRequest
	if err := validate.Request(r.Body, &req); err != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(err)))
		return
	}

	principal, _ := middleware.PrincipalFromContext(r.Context())
	if err := s.changePassword(r.Context(), id, req.CurrentPassword, req.NewPassword, principal.SessionID); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"password_changed": true})
}

func (s *Service) listSessions(w http.ResponseWriter, r *http.Request) {
	id, err := principalUserID(r)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	sessions, err := s.sessions.ListForUser(r.Context(), id)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]sessionView, 0, len(sessions))
	for _, se := range sessions {
		views = append(views, sessionView{
			ID:          se.ID,
			Provider:    se.Provider,
			UserAgent:   se.UserAgent,
			DeviceName:  se.DeviceName,
			IPAddress:   se.IPAddress,
			CreatedAt:   se.CreatedAt,
			ExpiresAt:   se.ExpiresAt,
			RefreshedAt: se.RefreshedAt,
		})
	}
	responder.Success(w, r, http.StatusOK, views)
}

func (s *Service) revokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := principalUserID(r)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	if err := s.sessions.RevokeForUser(r.Context(), id, chi.URLParam(r, "id")); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"revoked": true})
}

// writeError maps module errors to the response envelope.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, session.ErrNotFound):
		responder.NotFoundJSON(w, r)
	case errors.Is(err, password.ErrInvalidCredentials):
		responder.Fail(w, r, http.StatusBadRequest, "current password is incorrect")
	case errors.Is(err, password.ErrWeakPassword):
		responder.Fail(w, r, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, user.ErrNotFound):
		responder.NotFoundJSON(w, r)
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
