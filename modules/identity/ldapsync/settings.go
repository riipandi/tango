package ldapsync

import "github.com/riipandi/tango/internal/config"

// FromMergedValues reads the merged appconfig values (env defaults
// already folded) into the sync settings. Bool parsing is lenient:
// anything but "true" is off, mirroring the env behavior.
func FromMergedValues(values map[string]string) LDAPSettings {
	return LDAPSettings{
		Enabled:           values["ldap_enabled"] == "true",
		URL:               values["ldap_url"],
		BindDN:            values["ldap_bind_dn"],
		BindPassword:      values["ldap_bind_password"],
		Base:              values["ldap_base"],
		UserFilter:        values["ldap_user_search_filter"],
		GroupFilter:       values["ldap_user_group_search_filter"],
		SkipCertVerify:    values["ldap_skip_cert_verify"] == "true",
		AttrUserUniqueID:  values["ldap_attribute_user_unique_identifier"],
		AttrUserUsername:  values["ldap_attribute_user_username"],
		AttrUserEmail:     values["ldap_attribute_user_email"],
		AttrUserFirstName: values["ldap_attribute_user_first_name"],
		AttrUserLastName:  values["ldap_attribute_user_last_name"],
		AttrUserDisplay:   values["ldap_attribute_user_display_name"],
		AttrGroupUniqueID: values["ldap_attribute_group_unique_identifier"],
		AttrGroupName:     values["ldap_attribute_group_name"],
		AttrGroupMember:   values["ldap_attribute_group_member"],
		AdminGroupName:    values["ldap_admin_group_name"],
		SoftDeleteUsers:   values["ldap_soft_delete_users"] == "true",
	}
}

// SettingsFromEnv maps env config onto the sync settings as the
// pre-appconfig fallback (settingsSource not attached yet).
func SettingsFromEnv(cfg *config.Config) LDAPSettings {
	return LDAPSettings{
		Enabled:           cfg.LDAP.Enabled,
		URL:               cfg.LDAP.URL,
		BindDN:            cfg.LDAP.BindDN,
		BindPassword:      cfg.LDAP.BindPassword,
		Base:              cfg.LDAP.Base,
		UserFilter:        cfg.LDAP.UserFilter,
		GroupFilter:       cfg.LDAP.GroupFilter,
		SkipCertVerify:    cfg.LDAP.SkipCertVerify,
		AttrUserUniqueID:  cfg.LDAP.AttrUserUniqueID,
		AttrUserUsername:  cfg.LDAP.AttrUserUsername,
		AttrUserEmail:     cfg.LDAP.AttrUserEmail,
		AttrUserFirstName: cfg.LDAP.AttrUserFirstName,
		AttrUserLastName:  cfg.LDAP.AttrUserLastName,
		AttrUserDisplay:   cfg.LDAP.AttrUserDisplay,
		AttrGroupUniqueID: cfg.LDAP.AttrGroupUniqueID,
		AttrGroupName:     cfg.LDAP.AttrGroupName,
		AttrGroupMember:   cfg.LDAP.AttrGroupMember,
		AdminGroupName:    cfg.LDAP.AdminGroupName,
		SoftDeleteUsers:   cfg.LDAP.SoftDeleteUsers,
	}
}
