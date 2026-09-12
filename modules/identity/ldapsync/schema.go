// Package ldapsync periodically syncs users and groups from an LDAP
// directory on a scheduler; settings come from appconfig. Runs via
// kernel.Startable.
package ldapsync

import (
	"github.com/riipandi/tango/modules/identity"
)

// Feature is the wireable ldapsync unit.
type Feature struct{}

// New returns the placeholder feature.
func New() Feature { return Feature{} }

// Name implements identity.Feature.
func (Feature) Name() string { return "ldapsync" }

var _ identity.Feature = Feature{}
