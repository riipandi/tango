// Package password is the password authentication subdomain of
// identity: an alternative sign-in method alongside passkeys (which
// remain primary). Covers credential storage, password login,
// self/admin password change, and email-based forgot/reset flows.
//
// Contracts (per project convention):
//
//	PasswordCredential — hashed credential bound to a user
//	Hasher             — hash/verify abstraction (argon2id)
//	policy errors      — ErrWeakPassword, ErrInvalidCredentials
//
// Planned files: handler.go, service.go, store.go (see below).
// Password policy is read from appconfig; reset emails go through the
// mailer with single-use tokens (same semantics as onetimeaccess).
package password

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the password subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (sessions, hasher, policy, store) when the
// subdomain is implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "password" }

// Compile-time contract check: the feature satisfies the identity
// feature contract.
var _ identity.Feature = Feature{}
