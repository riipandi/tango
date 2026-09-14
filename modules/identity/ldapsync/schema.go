// Package ldapsync reconciles users and user groups from an LDAP
// directory: fetch the desired state (users + groups with members),
// then create/update/disable/delete inside one transaction. Settings
// arrive as an LDAPSettings value resolved by the caller (appconfig
// once it lands, env until then). A manual trigger endpoint and a
// recurring ticker share the same SyncAll path.
package ldapsync

import (
	"errors"
	"strings"
)

// LDAPSettings carries the directory connection and attribute map.
// Zero-value attribute fields fall back to the documented defaults.
type LDAPSettings struct {
	Enabled        bool
	URL            string // ldap://host:port or ldaps://
	BindDN         string
	BindPassword   string
	Base           string
	UserFilter     string // default (objectClass=person)
	GroupFilter    string // default (objectClass=groupOfNames)
	SkipCertVerify bool

	AttrUserUniqueID  string // default uid
	AttrUserUsername  string // default uid
	AttrUserEmail     string // default mail
	AttrUserFirstName string // default givenName
	AttrUserLastName  string // default sn
	AttrUserDisplay   string // default displayName

	AttrGroupUniqueID string // default cn
	AttrGroupName     string // default cn
	AttrGroupMember   string // default member

	AdminGroupName  string // group whose members become admins
	SoftDeleteUsers bool   // disable instead of delete on removal
}

// defaults fills unset attribute names.
func (s LDAPSettings) withDefaults() LDAPSettings {
	set := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return v
	}
	s.UserFilter = set(s.UserFilter, "(objectClass=person)")
	s.GroupFilter = set(s.GroupFilter, "(objectClass=groupOfNames)")
	s.AttrUserUniqueID = set(s.AttrUserUniqueID, "uid")
	s.AttrUserUsername = set(s.AttrUserUsername, "uid")
	s.AttrUserEmail = set(s.AttrUserEmail, "mail")
	s.AttrUserFirstName = set(s.AttrUserFirstName, "givenName")
	s.AttrUserLastName = set(s.AttrUserLastName, "sn")
	s.AttrUserDisplay = set(s.AttrUserDisplay, "displayName")
	s.AttrGroupUniqueID = set(s.AttrGroupUniqueID, "cn")
	s.AttrGroupName = set(s.AttrGroupName, "cn")
	s.AttrGroupMember = set(s.AttrGroupMember, "member")
	return s
}

// errors surfaced to callers.
var (
	ErrDisabled      = errors.New("ldapsync: LDAP sync is disabled")
	ErrNotConfigured = errors.New("ldapsync: LDAP is not configured")
)
