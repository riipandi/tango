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

// RateLimitStore runs the fixed-window rate check.
type RateLimitStore interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Rate limit classes.
const (
	RateClassDefault = "default"
	RateClassAuth    = "auth"
)

// classLimits maps each class to a request limit and window in seconds.
var classLimits = map[string]struct {
	Max    int
	Window int
}{
	RateClassDefault: {100, 900},
	RateClassAuth:    {20, 60},
}

// RateLimit counts requests per client IP; the class comes from the
// request path, so sensitive auth routes get the tighter budget.
func RateLimit(store RateLimitStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			class := ClassForPath(r.URL.Path)
			limits := classLimits[class]
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
				// Allow the request when the store is unavailable.
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

// ClassForPath maps a request path to its rate-limit class. Sensitive
// authentication surfaces share the tight auth budget; everything
// else rides the default budget.
func ClassForPath(path string) string {
	for _, prefix := range authPrefixes {
		if strings.HasPrefix(path, prefix) {
			return RateClassAuth
		}
	}
	return RateClassDefault
}

// authPrefixes lists the paths the tight auth budget protects: the
// sign-in surfaces plus every token-minting or token-exchanging
// endpoint.
var authPrefixes = []string{
	"/api/auth/",
	"/api/oidc/token",
	"/api/signup",
	"/api/one-time-access-email",
	"/api/one-time-access-token/",
	"/api/device-login/",
	"/api/webauthn/",
	"/api/users/me/send-email-verification",
	"/api/users/me/verify-email",
}

// rateKey builds the database key for a client IP and class.
func rateKey(r *http.Request, class string) string {
	ip := clientIP(r)
	if ip == "" {
		return ""
	}
	ip = strings.ReplaceAll(ip, ".", "_")
	ip = strings.ReplaceAll(ip, ":", "_")
	return fmt.Sprintf("rl_%s_%s", class, ip)
}

// clientIP resolves the peer and trusts forwarded IPs from loopback only.
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

// writeRateLimited writes a 429 response with Retry-After when available.
func writeRateLimited(w http.ResponseWriter, r *http.Request, detail string) {
	if seconds := retryAfter(detail); seconds > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
	}
	responder.Fail(w, r, http.StatusTooManyRequests, "rate limit exceeded")
}

// retryAfter extracts seconds from a database detail string.
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
