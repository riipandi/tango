// Package fetcher is the shared HTTP client for outbound service
// integrations, built on resty. It centralizes the settings every
// outbound call should have: an honest, sanitized User-Agent,
// a request timeout, JSON helpers with non-2xx surfaced as errors,
// and one structured log entry per request.
package fetcher

import (
	"time"

	"github.com/riipandi/tango/internal/logger"
)

// DefaultTimeout bounds each request when Options.Timeout is unset.
const DefaultTimeout = 10 * time.Second

// DefaultRetries is the retry count after the first attempt when
// Options.Retries is unset.
const DefaultRetries = 2

// Breaker defaults for the opt-in circuit breaker.
const (
	DefaultBreakerThreshold = 5                // failures before opening
	DefaultBreakerSuccess   = 1                // successful probes to close
	DefaultBreakerReset     = 30 * time.Second // open duration
)

// Options parametrizes New.
type Options struct {
	// BaseURL prefixes every request URL when set — for a Fetcher
	// dedicated to a single upstream. The shared instance leaves it
	// empty: call sites pass absolute URLs.
	BaseURL string

	// Timeout bounds each request. Zero uses DefaultTimeout.
	Timeout time.Duration

	// Logger receives one structured entry per outbound request
	// (debug on 2xx, warning on 4xx, error on 5xx and transport
	// failures). Nil keeps the fetcher silent.
	Logger logger.Logger

	// Debug enables resty's internal request/response dump for
	// troubleshooting; the dump flows through the shared slog
	// instance when available.
	Debug bool

	// Retries is the number of retries after the first attempt for
	// idempotent requests (GET, HEAD, PUT, DELETE, OPTIONS, TRACE)
	// on 429/5xx and temporary transport failures — exponential
	// backoff with jitter. Zero uses DefaultRetries; negative
	// disables retrying. POST/PATCH are never retried unless the
	// caller opts in per request (resty's SetRetryAllowNonIdempotent).
	Retries int

	// RetryWaitTime and RetryMaxWaitTime bound the backoff delay
	// between retries. Zero values use resty's defaults (100ms
	// minimum, 2s maximum).
	RetryWaitTime    time.Duration
	RetryMaxWaitTime time.Duration

	// CircuitBreaker enables a count-based breaker (5 failures in
	// the sliding window opens it; one successful probe closes it
	// after DefaultBreakerReset). Opt-in: the fetcher is a shared
	// client, so a breaker here trips for every integration at
	// once — per-upstream breakers want dedicated Fetcher
	// instances.
	CircuitBreaker bool
}
