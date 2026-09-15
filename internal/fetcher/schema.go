// Package fetcher provides the shared outbound HTTP client.
package fetcher

import (
	"time"

	"github.com/riipandi/tango/internal/logger"
)

// DefaultTimeout is used when Options.Timeout is unset.
const DefaultTimeout = 10 * time.Second

// DefaultRetries is the retry count when Options.Retries is zero.
const DefaultRetries = 2

// Circuit-breaker defaults.
const (
	DefaultBreakerThreshold = 5                // failures to open
	DefaultBreakerSuccess   = 1                // probes to close
	DefaultBreakerReset     = 30 * time.Second // open duration
)

// Options configures New.
type Options struct {
	// BaseURL prefixes requests. Empty means callers use absolute URLs.
	BaseURL string

	// Timeout bounds each request. Zero uses DefaultTimeout.
	Timeout time.Duration

	// Logger receives one entry per request. Nil disables logging.
	Logger logger.Logger

	// Debug enables resty's request/response dump.
	Debug bool

	// Retries is the retry count for idempotent requests. Zero uses
	// DefaultRetries; negative disables retries.
	Retries int

	// RetryWaitTime and RetryMaxWaitTime bound retry backoff.
	RetryWaitTime    time.Duration
	RetryMaxWaitTime time.Duration

	// CircuitBreaker enables a count-based breaker for this client.
	CircuitBreaker bool
}
