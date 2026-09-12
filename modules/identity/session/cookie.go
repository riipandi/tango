package session

import (
	"net"
	"net/http"
	"time"
)

// CookieName is the browser session cookie.
const CookieName = "tango_session"

// WriteCookie stores the session token: HttpOnly (no script access),
// SameSite=Lax (CSRF-safe for top-level navigation), Secure except in
// plain development mode, path-scoped to the whole site.
func WriteCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	// Secure follows the run mode (dev is plain HTTP); HttpOnly and
	// SameSite are always set.
	// #nosec G124
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// ClearCookie expires the session cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	// Secure follows the run mode (dev is plain HTTP); HttpOnly and
	// SameSite are always set.
	// #nosec G124
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// RequestIP extracts the client IP for session metadata; proxy
// headers are trusted only via the transport chain, so the socket
// peer is the source of truth here.
func RequestIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
