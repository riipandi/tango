package middleware

import (
	"context"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/riipandi/tango/pkg/responder"
)

// The headers a limited client reads back, and the ones the responder's
// envelope metadata copies into every response that follows.
const (
	RateLimitLimitHeader     = "X-RateLimit-Limit"
	RateLimitRemainingHeader = "X-RateLimit-Remaining"
	RateLimitResetHeader     = "X-RateLimit-Reset"
	RateLimitRetryHeader     = "Retry-After"
)

// Limiter answers one rate limit check for one key. A key is opaque here; the
// drivers give it its backend and its window.
type Limiter interface {
	Allow(ctx context.Context, key string) (Result, error)
}

// Result is the outcome of one check. A limited result carries a RetryAfter
// the middleware turns into the header the client asked for.
type Result struct {
	Limited    bool
	Limit      int
	Remaining  int
	ResetAt    time.Time
	RetryAfter time.Duration
}

// RateLimit throttles the requests a client may make, keyed by its address.
//
// A limited request is refused with a 429 envelope and a Retry-After header
// before any route runs; every response carries the X-RateLimit-* headers, so
// a well-behaved client can pace itself and the envelope metadata the
// responder publishes stays filled.
//
// The excluded prefixes are the paths the limiter never counts — the health
// endpoint, a webhook a partner posts to. They are named by the caller at
// the mount site, in the one list the router composes its pipeline from. A
// prefix matches the paths under it, so an exclusion of "/api/healthz" also
// spares "/api/healthz/deep".
//
// A limiter that cannot answer — a database that is down — lets the request
// through. The limiter is a protection of the service, and taking the API
// down to enforce it inverts the relationship: a degraded backend costs some
// throttling, never the service itself. The driver logs its own failures.
//
// The key is the connection's host without its port. Behind a proxy every
// address is the proxy's, which makes the limit global rather than per
// client; a deployment that terminates TLS on the application itself gets
// honest keys.
func RateLimit(limiter Limiter, excluded ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if limiter == nil {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// An excluded path is answered before the check runs, so a probe
			// costs the backend no round trip at all, not merely one it
			// would have passed.
			for _, prefix := range excluded {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}

			result, err := limiter.Allow(r.Context(), rateLimitKey(r))
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set(RateLimitLimitHeader, strconv.Itoa(result.Limit))
			w.Header().Set(RateLimitRemainingHeader, strconv.Itoa(max(result.Remaining, 0)))
			if !result.ResetAt.IsZero() {
				w.Header().Set(RateLimitResetHeader, strconv.FormatInt(result.ResetAt.Unix(), 10))
			}

			if result.Limited {
				seconds := max(int64(result.RetryAfter/time.Second), 1)
				w.Header().Set(RateLimitRetryHeader, strconv.FormatInt(seconds, 10))
				responder.Fail(w, r, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rateLimitKey names the bucket one client falls into. The form has to satisfy
// the rate_limits key check — lowercase alphanumerics, underscores, colons —
// which is why an address loses its dots before it becomes a key.
func rateLimitKey(r *http.Request) string {
	return "ip_" + sanitizeKey(clientHost(r))
}

// sanitizeKey reduces any string to the alphabet the rate_limits check allows.
func sanitizeKey(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == ':':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '_'
		}
	}, s)
}

// remoteKey is the address the request came from, port stripped; a failure to
// split leaves the address whole, which sanitizeKey can still reduce.

// retryAfterFromDetail reads the retry hint the check function raises with its
// SQLSTATE. The detail is a rendered sentence, so the parse is best effort:
// the window is the fallback, and a mismatch costs a client one polite wait.
var retryAfterPattern = regexp.MustCompile(`Retry after:\s*(\d+)`)

func retryAfterFromDetail(detail string, window time.Duration) time.Duration {
	if match := retryAfterPattern.FindStringSubmatch(detail); match != nil {
		if seconds, err := strconv.ParseInt(match[1], 10, 64); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	return window
}
