// Package apiaccess is the machine authorization subdomain of
// identity: API definitions with scopes/permissions and machine-client
// grants. The oidc module evaluates machine-token access through a
// consumer-side adapter (same pattern as identity.Recorder), so this
// package never imports the oidc module.
//
// Planned files: handler.go (/api/apis CRUD, /api/api-access),
// service.go, store.go.
package apiaccess
