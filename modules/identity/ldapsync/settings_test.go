package ldapsync

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFromMergedValuesReadsMergedValues(t *testing.T) {
	settings := FromMergedValues(map[string]string{
		"ldap_enabled":       "true",
		"ldap_url":           "ldaps://directory.example",
		"ldap_bind_dn":       "cn=sync",
		"ldap_bind_password": "secret",
		"ldap_base":          "dc=example,dc=org",
	})
	assert.True(t, settings.Enabled)
	assert.Equal(t, "ldaps://directory.example", settings.URL)
	assert.Equal(t, "cn=sync", settings.BindDN)
	assert.Equal(t, "secret", settings.BindPassword)
	assert.Equal(t, "dc=example,dc=org", settings.Base)

	// Bool parsing is lenient: anything but "true" is off.
	assert.False(t, FromMergedValues(map[string]string{"ldap_enabled": "1"}).Enabled)
}
