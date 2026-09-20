package session

import (
	"net"
	"net/http"
)

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
