package session

import (
	"net/http"
	"time"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
)

// tokenBridge is POST /api/auth/token — the auth worker's bootstrap
// and refresh channel. Same-origin fetch with credentials: include
// brings the access and refresh cookies; the answer is a fresh
// bearer access token for RPC injection. A valid access cookie is
// returned as-is; otherwise the refresh token rotates (the old
// token stops resolving) and both cookies re-issue.
func (s *Service) tokenBridge(w http.ResponseWriter, r *http.Request) {
	if s.tokens == nil {
		responder.Fail(w, r, http.StatusNotImplemented, "token bridge is not configured")
		return
	}

	// Fast path: the access cookie still verifies and the owning
	// session lives — no rotation needed.
	if raw := cookieValue(r, AccessTokenCookieName); raw != "" {
		if verified, err := s.tokens.Verify(r.Context(), raw); err == nil {
			if se, u, err := s.store.ValidByID(r.Context(), verified.Private.SessionID); err == nil && !u.Disabled {
				writeTokenBridge(w, r, raw, verified.ExpiresAt, se.ExpiresAt, nil, s.cookieSecure)
				return
			}
		}
	}

	refresh := cookieValue(r, CookieName)
	if refresh == "" {
		responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired token")
		return
	}
	u, se, err := s.Resolve(r.Context(), refresh)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	principal := kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}
	newRefresh, rotated, err := s.Rotate(r.Context(), se)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	access, expiresAt, err := s.IssueAccess(r.Context(), principal)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	writeTokenBridge(w, r, access, expiresAt, rotated.ExpiresAt, &newRefresh, s.cookieSecure)
}

// writeTokenBridge answers the bridge with the bearer token and
// mirrors it (plus the rotated refresh token) into cookies.
func writeTokenBridge(w http.ResponseWriter, r *http.Request, access string, accessExpires time.Time, refreshExpires time.Time, newRefresh *string, secure bool) {
	writeAccessCookie(w, access, accessExpires, secure)
	if newRefresh != nil {
		WriteCookie(w, *newRefresh, refreshExpires, secure)
	}
	responder.Success(w, r, http.StatusOK, tokenBridgeResponse{
		AccessToken: access,
		TokenType:   "Bearer",
		ExpiresIn:   int(time.Until(accessExpires).Seconds()),
		ExpiresAt:   accessExpires,
	})
}

// tokenBridgeResponse is the worker-facing bootstrap payload; the
// refresh token never appears in a body.
type tokenBridgeResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int       `json:"expires_in"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// writeAccessCookie mirrors the access token into its cookie.
func writeAccessCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	http.SetCookie(w, accessCookie(token, expires, secure))
}

// clearAccessCookie expires the access cookie.
func clearAccessCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, expiredCookie(AccessTokenCookieName, AccessTokenPath, secure))
}

// cookieValue reads a named cookie, empty when absent.
func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}
