// Schema contracts and shared vocabulary of the identity module:
// interfaces live here so alternate implementations (memory, Postgres)
// stay swappable without touching handlers or business rules.
//
// The identity module owns all authn/authz concerns. Subdomains:
//
//	user (core) — accounts + CRUD; mandatory core feature
//	account   — profile, credentials, account state
//	session   — sign-in sessions (issue, refresh, revoke)
//	webauthn  — passkey credentials + ceremonies (primary authn)
//	password  — password credentials + login/reset flows
//	apikey    — machine credentials (X-API-KEY)
//	apiaccess — machine authorization (API scopes + client grants)
//	oidc      — OpenID Connect / OAuth 2.0 provider surface
//	            (authorize, token, userinfo; one flat package,
//	            file-per-area: client, token, device)
//	usergroup — bulk OIDC client access via groups
//	customclaim — per-user claims injected into OIDC tokens
//	signup    — self-service sign-up flows
//	emailverification — verify email addresses
//	onetimeaccess — email sign-in links (single-use tokens)
//	devicelogin — verify a sign-in code on a limited-input device
package identity

import (
	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
)

// User is the identity module's core entity. It lives at the root
// (not in a subpackage) because every feature and the oidc surface
// share this vocabulary.
type User struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Recorder receives audit events from identity features. It is a
// function type so the composition root can adapt any sink (e.g. the
// auditlog module) without features importing that module.
type Recorder func(event AuditEvent)

// AuditEvent is what identity features emit to a recorder. Declared
// here (not imported from auditlog) so modules stay decoupled.
type AuditEvent struct {
	Action string
	Actor  string
	Target string
}

// Feature is one selectable unit of the identity module: an
// authn/authz capability (password, webauthn, apikey, ...) chosen at
// the composition root. A feature left out of New simply does not
// exist: no routes, no storage, no lifecycle.
type Feature interface {
	Name() string
}

// APIFeature is the optional capability to mount endpoints inside the
// shared /api group, alongside the module's own routes. The mandatory
// user core implements this too.
type APIFeature interface {
	Feature
	APIRoutes(r chi.Router)
}

// StartableFeature is the optional lifecycle capability for features
// holding resources. Started after the module core, stopped before it
// (reverse feature order).
type StartableFeature interface {
	Feature
	kernel.Startable
}

// RootRoutableFeature is the optional capability to mount routes on
// the root router (outside /api) — e.g. the OIDC /authorize endpoint.
type RootRoutableFeature interface {
	Feature
	Routes(r chi.Router)
}
