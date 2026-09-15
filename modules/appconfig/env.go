package appconfig

import (
	"encoding/json"
	"strconv"

	"github.com/riipandi/tango/internal/config"
)

// env.go maps internal/config sections onto the admin-editable keys:
// the env layer of the defaults fold, and parsed projections of the
// merged values for cross-module consumers.

// EnvDefaults maps the koanf LDAP/Mailer sections onto the appconfig
// keys as the env layer of the defaults fold.
func EnvDefaults(cfg *config.Config) map[string]string {
	return map[string]string{
		"smtp_from_email":                        cfg.Mailer.FromEmail,
		"smtp_from_name":                         cfg.Mailer.FromName,
		"smtp_host":                              cfg.Mailer.SMTPHost,
		"smtp_port":                              strconv.Itoa(cfg.Mailer.SMTPPort),
		"smtp_username":                          cfg.Mailer.SMTPUsername,
		"smtp_password":                          cfg.Mailer.SMTPPassword,
		"smtp_secure":                            strconv.FormatBool(cfg.Mailer.SMTPSecure),
		"ldap_enabled":                           strconv.FormatBool(cfg.LDAP.Enabled),
		"ldap_url":                               cfg.LDAP.URL,
		"ldap_bind_dn":                           cfg.LDAP.BindDN,
		"ldap_bind_password":                     cfg.LDAP.BindPassword,
		"ldap_base":                              cfg.LDAP.Base,
		"ldap_user_search_filter":                cfg.LDAP.UserFilter,
		"ldap_user_group_search_filter":          cfg.LDAP.GroupFilter,
		"ldap_skip_cert_verify":                  strconv.FormatBool(cfg.LDAP.SkipCertVerify),
		"ldap_attribute_user_unique_identifier":  cfg.LDAP.AttrUserUniqueID,
		"ldap_attribute_user_username":           cfg.LDAP.AttrUserUsername,
		"ldap_attribute_user_email":              cfg.LDAP.AttrUserEmail,
		"ldap_attribute_user_first_name":         cfg.LDAP.AttrUserFirstName,
		"ldap_attribute_user_last_name":          cfg.LDAP.AttrUserLastName,
		"ldap_attribute_user_display_name":       cfg.LDAP.AttrUserDisplay,
		"ldap_attribute_group_unique_identifier": cfg.LDAP.AttrGroupUniqueID,
		"ldap_attribute_group_name":              cfg.LDAP.AttrGroupName,
		"ldap_attribute_group_member":            cfg.LDAP.AttrGroupMember,
		"ldap_admin_group_name":                  cfg.LDAP.AdminGroupName,
		"ldap_soft_delete_users":                 strconv.FormatBool(cfg.LDAP.SoftDeleteUsers),
	}
}

// CIMDAllowlist reads the operator-managed CIMD URL allowlist from
// the merged values (JSON array; default deny when unset).
func CIMDAllowlist(values map[string]string) []string {
	var allowlist []string
	_ = json.Unmarshal([]byte(values["cimd_url_allowlist"]), &allowlist)
	return allowlist
}
