package session

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// APIRoutes mounts the retained auth endpoints inside the shared /api
// group: the worker's cookie bridge and the cookie-channel sign-out
// fallback. Sign-in and session reads serve ConnectRPC exclusively —
// see handler_rpc.go.
func (s *Service) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Post("/auth/token", s.tokenBridge)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(s, CookieName))
		r.Post("/auth/sign-out", s.signOut)
	})
}

// signOut revokes the caller's session and clears the cookie; any
// pending second-factor bridge dies with it. This REST form is the
// worker's documented fallback for when the RPC bearer path is
// unusable — the cookie channel must keep working.
func (s *Service) signOut(w http.ResponseWriter, r *http.Request) {
	if token := cookieToken(r); token != "" {
		if err := s.RevokeCurrent(r.Context(), token); err != nil {
			responder.Fail(w, r, http.StatusInternalServerError, "internal error")
			return
		}
	}
	if principal, ok := middleware.PrincipalFromContext(r.Context()); ok && s.mfa != nil {
		_ = s.mfa.ClearPending(r.Context(), principal.UserID)
	}
	ClearCookie(w, s.cookieSecure)
	clearAccessCookie(w, s.cookieSecure)
	clearPendingCookie(w, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, map[string]any{"signed_out": true})
}

// cookieToken reads the raw session token, empty when absent.
func cookieToken(r *http.Request) string {
	cookie, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return cookie.Value
}
