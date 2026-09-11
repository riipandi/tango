// Package ldapsync is periodic LDAP directory synchronization (users +
// groups) driven by an internal/job schedule and appconfig-managed
// LDAP settings.
//
// Implements kernel.Startable to run its scheduler. Planned files:
// service.go (bind, search, upsert), store.go.
package ldapsync
