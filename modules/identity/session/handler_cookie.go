package session

import (
	"net"
	"net/http"
	"time"

	"github.com/riipandi/tango/modules/identity"
)

// CookieName is the browser refresh-token cookie (the rotating
// session token; the family row keeps its hash).
const CookieName = "tango_session"

// sessionCookie builds the refresh-token cookie: HttpOnly (no
// script access), SameSite=Lax (CSRF-safe for top-level navigation),
// Secure except in plain development mode, path-scoped to the whole
// site.
func sessionCookie(token string, expires time.Time, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// pendingCookie builds the short-lived pending-auth cookie: the
// bridge token for MFA verification, never a session token.
func pendingCookie(token string, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     identity.PendingCookieName,
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(identity.PendingCookieTTL),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// clearPendingCookie expires the pending-auth cookie.
func clearPendingCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, expiredCookie(identity.PendingCookieName, "/", secure))
}

// accessCookie mirrors the access token: HttpOnly, scoped to the
// bridge path, lifetime bounded by the token expiry.
func accessCookie(token string, expires time.Time, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     AccessTokenCookieName,
		Value:    token,
		Path:     AccessTokenPath,
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// expiredCookie builds the clearing form of a cookie.
func expiredCookie(name string, path string, secure bool) *http.Cookie {
	// #nosec G124 -- Secure mirrors the run mode (SameSite=Lax)
	return &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     path,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// WriteCookie stores the refresh token.
func WriteCookie(w http.ResponseWriter, token string, expires time.Time, secure bool) {
	http.SetCookie(w, sessionCookie(token, expires, secure))
}

// WritePendingCookie stores the MFA bridge token.
func WritePendingCookie(w http.ResponseWriter, token string, secure bool) {
	http.SetCookie(w, pendingCookie(token, secure))
}

// ClearCookie expires the refresh-token cookie.
func ClearCookie(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, expiredCookie(CookieName, "/", secure))
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
