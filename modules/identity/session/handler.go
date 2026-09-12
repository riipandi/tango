package session

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// signInRequest is the POST /auth/sign-in payload.
type signInRequest struct {
	Identity string `json:"identity"`
	Secret   string `json:"secret"`
}

func (r signInRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Identity, validation.Required),
		validation.Field(&r.Secret, validation.Required),
	)
}

// signInResponse echoes the signed-in user and the session header.
type signInResponse struct {
	User      user.User `json:"user"`
	SessionID string    `json:"session_id"`
	Provider  string    `json:"provider"`
	ExpiresAt time.Time `json:"expires_at"`
}

// APIRoutes mounts the auth endpoints inside the shared /api group:
// public sign-in, then cookie-guarded session management.
func (s *Service) APIRoutes(r chi.Router) {
	r.Post("/auth/sign-in", s.signIn)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(s, CookieName))
		r.Post("/auth/sign-out", s.signOut)
		r.Get("/auth/session", s.current)
	})
}

// signIn verifies credentials and sets the session cookie.
func (s *Service) signIn(w http.ResponseWriter, r *http.Request) {
	var req signInRequest
	if err := validate.Request(r.Body, &req); err != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(err)))
		return
	}

	meta := Meta{
		UserAgent: r.UserAgent(),
		IPAddress: RequestIP(r),
	}
	token, u, se, err := s.SignIn(r.Context(), req.Identity, req.Secret, meta)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "invalid credentials")
		return
	}

	WriteCookie(w, token, se.ExpiresAt, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, signInResponse{
		User:      u,
		SessionID: se.ID,
		Provider:  se.Provider,
		ExpiresAt: se.ExpiresAt,
	})
}

// signOut revokes the caller's session and clears the cookie.
func (s *Service) signOut(w http.ResponseWriter, r *http.Request) {
	if token := cookieToken(r); token != "" {
		if err := s.RevokeCurrent(r.Context(), token); err != nil {
			responder.Fail(w, r, http.StatusInternalServerError, "internal error")
			return
		}
	}
	ClearCookie(w, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, map[string]any{"signed_out": true})
}

// current echoes the live session and its user.
func (s *Service) current(w http.ResponseWriter, r *http.Request) {
	principal, _ := middleware.PrincipalFromContext(r.Context())

	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	u, err := s.users.GetByID(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired session")
		return
	}

	responder.Success(w, r, http.StatusOK, signInResponse{
		User:      u,
		SessionID: principal.SessionID,
		Provider:  principal.Provider,
	})
}

// cookieToken reads the raw session token, empty when absent.
func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
