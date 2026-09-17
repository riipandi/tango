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

// Policy is a fixed-window budget for one sensitive endpoint.
type Policy struct {
	// Name separates the per-IP counter keys so budgets stay isolated
	// across endpoints.
	Name   string
	Max    int
	Window int // seconds
}

// policies holds every enforced budget. Only sensitive endpoints appear
// here; all other routes pass through unchecked. Budgets mirror the
// intent of upstream Pocket ID's per-route limiters, adapted to the
// tango-only surfaces.
var policies = map[string]Policy{
	"sign-in":                   {Name: "sign-in", Max: 20, Window: 60},
	"forgot-password":           {Name: "forgot-password", Max: 2, Window: 600},
	"reset-password":            {Name: "reset-password", Max: 5, Window: 600},
	"totp-enroll":               {Name: "totp-enroll", Max: 5, Window: 60},
	"totp-confirm":              {Name: "totp-confirm", Max: 10, Window: 60},
	"totp-verify":               {Name: "totp-verify", Max: 10, Window: 60},
	"totp-recovery-codes":       {Name: "totp-recovery-codes", Max: 5, Window: 60},
	"totp-disable":              {Name: "totp-disable", Max: 5, Window: 60},
	"signup":                    {Name: "signup", Max: 5, Window: 60},
	"signup-setup":              {Name: "signup-setup", Max: 5, Window: 60},
	"one-time-access-email":     {Name: "one-time-access-email", Max: 2, Window: 600},
	"one-time-access-token":     {Name: "one-time-access-token", Max: 10, Window: 60},
	"device-login-create":       {Name: "device-login-create", Max: 10, Window: 60},
	"device-login-exchange":     {Name: "device-login-exchange", Max: 30, Window: 60},
	"device-login-verify":       {Name: "device-login-verify", Max: 10, Window: 60},
	"device-login-decision":     {Name: "device-login-decision", Max: 10, Window: 60},
	"webauthn-login":            {Name: "webauthn-login", Max: 10, Window: 60},
	"webauthn-reauthenticate":   {Name: "webauthn-reauthenticate", Max: 6, Window: 60},
	"email-verification-send":   {Name: "email-verification-send", Max: 2, Window: 600},
	"email-verification-verify": {Name: "email-verification-verify", Max: 6, Window: 60},
}

// rule binds one endpoint shape to a policy. Matching requires the
// path to continue the prefix at a segment boundary (or end there), so
// /api/signup never shadows /api/signup-tokens. An optional suffix pins
// the tail, and an empty method matches any. Rules are evaluated in
// order and the first match wins.
var rules = []struct {
	Policy string
	Method string
	Prefix string
	Suffix string
}{
	{"sign-in", http.MethodPost, "/api/auth/sign-in", ""},
	{"forgot-password", http.MethodPost, "/api/auth/forgot-password", ""},
	{"reset-password", http.MethodPost, "/api/auth/reset-password", ""},
	{"totp-enroll", http.MethodPost, "/api/mfa/totp/enroll", ""},
	{"totp-confirm", http.MethodPost, "/api/mfa/totp/confirm", ""},
	{"totp-verify", http.MethodPost, "/api/mfa/totp/verify", ""},
	{"totp-recovery-codes", http.MethodPost, "/api/mfa/totp/recovery-codes", ""},
	{"totp-disable", http.MethodDelete, "/api/mfa/totp", ""},
	{"signup-setup", http.MethodPost, "/api/signup/setup", ""},
	{"signup", http.MethodPost, "/api/signup", ""},
	{"one-time-access-email", http.MethodPost, "/api/one-time-access-email", ""},
	{"one-time-access-token", http.MethodPost, "/api/one-time-access-token/", ""},
	{"device-login-exchange", http.MethodPost, "/api/device-login/requests/", "/exchange"},
	{"device-login-create", http.MethodPost, "/api/device-login/requests", ""},
	{"device-login-decision", http.MethodPost, "/api/device-login/verification/decision", ""},
	{"device-login-verify", http.MethodPost, "/api/device-login/verification", ""},
	{"webauthn-login", http.MethodPost, "/api/webauthn/login/finish", ""},
	{"webauthn-reauthenticate", http.MethodPost, "/api/webauthn/reauthenticate", ""},
	{"email-verification-send", http.MethodPost, "/api/users/me/send-email-verification", ""},
	{"email-verification-verify", http.MethodPost, "/api/users/me/verify-email", ""},
	{"one-time-access-email", http.MethodPost, "/api/users/", "/one-time-access-email"},
}

// PolicyFor returns the enforced policy for a request, or false when the
// route is not rate limited.
func PolicyFor(method, path string) (Policy, bool) {
	for _, rule := range rules {
		if rule.Method != "" && rule.Method != method {
			continue
		}
		rest, ok := matchPrefix(path, rule.Prefix)
		if !ok {
			continue
		}
		if rule.Suffix != "" {
			if !strings.HasSuffix(rest, rule.Suffix) {
				continue
			}
			rest = strings.TrimSuffix(rest, rule.Suffix)
		}
		if rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasSuffix(rule.Prefix, "/") {
			continue
		}
		return policies[rule.Policy], true
	}
	return Policy{}, false
}

// matchPrefix consumes the rule prefix and returns the remainder when
// the path continues at a segment boundary. A prefix that ends in its
// own slash already pins the boundary.
func matchPrefix(path, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	rest := path[len(prefix):]
	if rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasSuffix(prefix, "/") {
		return "", false
	}
	return rest, true
}

// RateLimit enforces the per-endpoint policies against a fixed-window
// store. Requests without a policy pass through without touching the
// store; a store failure fails open.
func RateLimit(store RateLimitStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			policy, ok := PolicyFor(r.Method, r.URL.Path)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			key := rateKey(r, policy.Name)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			var result []byte
			err := store.QueryRow(r.Context(),
				"SELECT fn_check_rate_limit($1, $2, $3)", key, policy.Max, policy.Window).Scan(&result)
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

// rateKey builds the database key for a client IP and policy.
func rateKey(r *http.Request, policy string) string {
	ip := clientIP(r)
	if ip == "" {
		return ""
	}
	ip = strings.ReplaceAll(ip, ".", "_")
	ip = strings.ReplaceAll(ip, ":", "_")
	return fmt.Sprintf("rl_%s_%s", policy, ip)
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
