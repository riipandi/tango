// Package ldapsync is the LDAP sync subdomain of identity:
// periodic directory sync (users + groups) on a scheduler,
// settings from appconfig. Runs via kernel.Startable.
package ldapsync

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable unit of the ldapsync subdomain.
type Feature struct{}

// New returns the placeholder feature. The real constructor will take
// its dependencies (scheduler, settings, users) when implemented.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "ldapsync" }

var _ identity.Feature = Feature{}
