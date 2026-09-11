// Package fetcher is the shared HTTP client for outbound service
// integrations, built on resty. It centralizes the settings every
// outbound call should have: an honest, sanitized User-Agent,
// a request timeout, and JSON helpers with non-2xx surfaced as
// errors.
package fetcher

import "time"

// DefaultTimeout bounds each request when Options.Timeout is unset.
const DefaultTimeout = 10 * time.Second

// Options parametrizes New.
type Options struct {
	// BaseURL is the application's public origin, included in the
	// User-Agent as the contact point (optional).
	BaseURL string

	// Timeout bounds each request. Zero uses DefaultTimeout.
	Timeout time.Duration
}
