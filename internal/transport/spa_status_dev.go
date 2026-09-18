//go:build !release

package transport

// spaFallbackStatus returns 404 in development.
const spaFallbackStatus = 404
