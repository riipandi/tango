// Package fetcher is the shared outbound HTTP client (resty):
// sane User-Agent, request timeout, JSON helpers with non-2xx as
// errors, one log entry per request.
package fetcher

import (
	"time"

	"github.com/riipandi/tango/internal/logger"
)

// DefaultTimeout bounds each request when Timeout is unset.
const DefaultTimeout = 10 * time.Second

// DefaultRetries is retries after first attempt when Retries unset.
const DefaultRetries = 2

// Breaker defaults for the opt-in circuit breaker.
const (
	DefaultBreakerThreshold = 5                // failures to open
	DefaultBreakerSuccess   = 1                // probes to close
	DefaultBreakerReset     = 30 * time.Second // open duration
)

// Options parametrizes New.
type Options struct {
	// BaseURL prefixes requests; empty for the shared instance
	// (callers pass absolute URLs).
	BaseURL string

	// Timeout bounds each request. Zero uses DefaultTimeout.
	Timeout time.Duration

	// Logger gets one entry per request (debug 2xx, warn 4xx,
	// error 5xx/transport). Nil silences.
	Logger logger.Logger

	// Debug enables resty's request/response dump.
	Debug bool

	// Retries after first attempt for idempotent requests
	// (GET/HEAD/PUT/DELETE/OPTIONS/TRACE) on 429/5xx and transient
	// failures, backoff with jitter. Zero uses DefaultRetries;
	// negative disables. POST/PATCH never retry unless opted in
	// per request.
	Retries int

	// RetryWaitTime/MaxWaitTime bound backoff. Zero uses resty
	// defaults (100ms min, 2s max).
	RetryWaitTime    time.Duration
	RetryMaxWaitTime time.Duration

	// CircuitBreaker enables a count-based breaker. Opt-in: on the
	// shared client it trips all integrations at once; per-upstream
	// breakers want dedicated Fetchers.
	CircuitBreaker bool
}
