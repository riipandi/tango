package middleware

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"

	jsonv2 "encoding/json/v2"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/riipandi/tango/pkg/responder"
)

// RateLimitStore executes the fixed-window check function; the
// datastore Store satisfies it directly.
type RateLimitStore interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Rate limit classes; auth endpoints use the tight class.
const (
	RateClassDefault = "default"
	RateClassAuth    = "auth"
)

// classLimits maps a class to (max requests, window seconds).
// Defaults mirror the config fallbacks; env tuning overrides them.
var classLimits = map[string]struct {
	Max    int
	Window int
}{
	RateClassDefault: {100, 900},
	RateClassAuth:    {20, 60},
}

// RateLimit returns middleware that counts requests per client IP
// and route class against the shared rate_limits table (multi-
// instance safe: the check function takes an advisory lock per key).
// Failures degrade open — an unavailable database must not take the
// whole surface down.
func RateLimit(store RateLimitStore, class string) func(http.Handler) http.Handler {
	limits, ok := classLimits[class]
	if !ok {
		limits = classLimits[RateClassDefault]
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := rateKey(r, class)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			var result []byte
			err := store.QueryRow(r.Context(),
				"SELECT fn_check_rate_limit($1, $2, $3)", key, limits.Max, limits.Window).Scan(&result)
			if err != nil {
				var pgErr *pgconn.PgError
				if errors.As(err, &pgErr) && pgErr.Code == "42901" {
					writeRateLimited(w, r, pgErr.Detail)
					return
				}
				// Degrade open on any other store failure.
				next.ServeHTTP(w, r)
				return
			}

			var info struct {
				Limit     int     `json:"limit"`
				Remaining int     `json:"remaining"`
				Reset     float64 `json:"reset"`
			}
			if jsonv2.Unmarshal(result, &info) == nil {
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(info.Limit))
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(info.Remaining))
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(int64(info.Reset), 10))
			}
			next.ServeHTTP(w, r)
		})
	}
}

// rateKey builds the per-IP key; the column CHECK restricts keys to
// lowercase alphanumerics, underscores, and colons.
func rateKey(r *http.Request, class string) string {
	ip := clientIP(r)
	if ip == "" {
		return ""
	}
	ip = strings.ReplaceAll(ip, ".", "_")
	ip = strings.ReplaceAll(ip, ":", "_")
	return fmt.Sprintf("rl_%s_%s", class, ip)
}

// clientIP resolves the direct peer, trusting X-Forwarded-For only
// from a loopback hop (local reverse proxy setups).
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}
		if isLoopback(remote) {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isLoopback(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// writeRateLimited answers 429 with Retry-After parsed from the
// check function's DETAIL ("Retry after: N seconds").
func writeRateLimited(w http.ResponseWriter, r *http.Request, detail string) {
	if seconds := retryAfter(detail); seconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	responder.Fail(w, r, http.StatusTooManyRequests, "rate limit exceeded")
}

// retryAfter extracts the seconds from a DETAIL string; 0 when
// absent or unparseable.
func retryAfter(detail string) int {
	marker := "Retry after: "
	_, after, ok := strings.Cut(detail, marker)
	if !ok {
		return 0
	}
	rest := after
	end := strings.IndexAny(rest, " ,")
	if end < 0 {
		end = len(rest)
	}
	seconds, err := strconv.Atoi(strings.TrimSpace(rest[:end]))
	if err != nil {
		return 0
	}
	return seconds
}
