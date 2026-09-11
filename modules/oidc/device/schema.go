// Package device is the OAuth 2.0 Device Authorization Grant subdomain
// (RFC 8628): limited-input devices poll for an access token while the
// user approves the code in a browser. Distinct from the identity
// devicelogin subdomain, which verifies a sign-in code on a device.
//
// Planned files: handler.go (/api/oidc/device), service.go, store.go.
package device
