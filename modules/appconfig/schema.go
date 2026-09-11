// Package appconfig is admin-editable application settings (branding,
// sign-up policy, session lifetimes, email/LDAP toggles) stored in the
// database and overlaid on top of internal/config env defaults.
//
// Planned files: handler.go (/api/app-config, admin-guarded),
// service.go, store.go.
package appconfig
