//go:build release

package transport

// spaFallbackStatus is the status for unknown page paths: release
// serves the embedded SPA shell for every non-API path.
const spaFallbackStatus = 200
