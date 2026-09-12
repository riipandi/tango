//go:build !release

package transport

// spaFallbackStatus is the status for unknown page paths: dev has
// no embedded shell, so the fallback is a plain 404.
const spaFallbackStatus = 404
