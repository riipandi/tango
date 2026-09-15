package appconfig

// config.go defines the admin-editable settings and their defaults.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// valueType constrains the stored string form of a key.
type valueType string

const (
	typeString valueType = "string"
	typeInt    valueType = "int"
	typeBool   valueType = "bool"
)

// signupModes lists the allowed signup policies.
var signupModes = []string{"disabled", "withToken", "open"}

// webauthnVerifications lists the allowed WebAuthn verification modes.
var webauthnVerifications = []string{"required", "preferred"}

// webauthnAttachments lists the allowed WebAuthn attachment modes.
var webauthnAttachments = []string{"any", "platform", "cross-platform"}

// configKey describes one editable setting.
type configKey struct {
	Key     string
	Type    valueType
	Public  bool
	Default string
	// OneOf restricts string values to a fixed set (empty = free).
	OneOf []string
	// Sensitive marks values that must be hidden in admin responses.
	Sensitive bool
}

// configKeys is the full catalog; keep Default in sync with
// internal/config + .env.example.
var configKeys = []configKey{
	{Key: "app_name", Type: typeString, Public: true, Default: "tango"},
	{Key: "session_duration", Type: typeInt, Default: "43200"}, // minutes; 30 days
	{Key: "home_page_url", Type: typeString, Public: true, Default: "/"},
	{Key: "accent_color", Type: typeString, Public: true},
	{Key: "disable_animations", Type: typeBool, Public: true},
	{Key: "allow_own_account_edit", Type: typeBool, Public: true, Default: "true"},
	{Key: "allow_user_signups", Type: typeString, Public: true, Default: "disabled", OneOf: signupModes},

	// Signup defaults are JSON-encoded.
	{Key: "signup_default_user_group_ids", Type: typeString},
	{Key: "signup_default_custom_claims", Type: typeString},

	// Email policy (SMTP relay settings live under the SMTP section)
	{Key: "require_user_email", Type: typeBool, Public: true},
	{Key: "email_login_notification_enabled", Type: typeBool, Default: "true"},
	{Key: "email_one_time_access_as_unauthenticated_enabled", Type: typeBool, Public: true},
	{Key: "email_one_time_access_as_admin_enabled", Type: typeBool, Public: true, Default: "true"},
	{Key: "email_api_key_expiration_enabled", Type: typeBool, Default: "true"},
	{Key: "email_verification_enabled", Type: typeBool, Public: true},

	// SMTP relay (defaults fold from MAILER_* env; password redacted)
	{Key: "smtp_from_email", Type: typeString, Default: "mailer@example.com"},
	{Key: "smtp_from_name", Type: typeString, Default: "MyApplication"},
	{Key: "smtp_host", Type: typeString, Default: "localhost"},
	{Key: "smtp_port", Type: typeInt, Default: "1025"},
	{Key: "smtp_username", Type: typeString},
	{Key: "smtp_password", Type: typeString, Sensitive: true},
	{Key: "smtp_secure", Type: typeBool},

	// LDAP directory sync (defaults fold from LDAP_* env; password
	// redacted; attribute keys fall back to the sync service defaults)
	{Key: "ldap_enabled", Type: typeBool, Public: true},
	{Key: "ldap_url", Type: typeString},
	{Key: "ldap_bind_dn", Type: typeString},
	{Key: "ldap_bind_password", Type: typeString, Sensitive: true},
	{Key: "ldap_base", Type: typeString},
	{Key: "ldap_user_search_filter", Type: typeString, Default: "(objectClass=person)"},
	{Key: "ldap_user_group_search_filter", Type: typeString, Default: "(objectClass=groupOfNames)"},
	{Key: "ldap_skip_cert_verify", Type: typeBool},
	{Key: "ldap_attribute_user_unique_identifier", Type: typeString, Default: "uid"},
	{Key: "ldap_attribute_user_username", Type: typeString, Default: "uid"},
	{Key: "ldap_attribute_user_email", Type: typeString, Default: "mail"},
	{Key: "ldap_attribute_user_first_name", Type: typeString, Default: "givenName"},
	{Key: "ldap_attribute_user_last_name", Type: typeString, Default: "sn"},
	{Key: "ldap_attribute_user_display_name", Type: typeString, Default: "displayName"},
	{Key: "ldap_attribute_group_unique_identifier", Type: typeString, Default: "cn"},
	{Key: "ldap_attribute_group_name", Type: typeString, Default: "cn"},
	{Key: "ldap_attribute_group_member", Type: typeString, Default: "member"},
	{Key: "ldap_admin_group_name", Type: typeString},
	{Key: "ldap_soft_delete_users", Type: typeBool},

	{Key: "webauthn_user_verification", Type: typeString, Default: "preferred", OneOf: webauthnVerifications},
	{Key: "webauthn_allow_synced_passkeys", Type: typeBool, Default: "true"},
	{Key: "webauthn_authenticator_attachment", Type: typeString, Default: "any", OneOf: webauthnAttachments},

	{Key: "cimd_url_allowlist", Type: typeString},
}

// lookup finds a catalog entry by key.
func lookup(key string) (configKey, bool) {
	for _, entry := range configKeys {
		if entry.Key == key {
			return entry, true
		}
	}
	return configKey{}, false
}

// validateValue checks one wire value against its declared type.
func validateValue(entry configKey, value string) error {
	switch entry.Type {
	case typeInt:
		if _, err := strconv.Atoi(value); err != nil {
			return fmt.Errorf("%s must be an integer", entry.Key)
		}
	case typeBool:
		switch strings.ToLower(value) {
		case "true", "false":
		default:
			return fmt.Errorf("%s must be true or false", entry.Key)
		}
	case typeString:
		if len(entry.OneOf) > 0 && value != "" && !slicesContains(entry.OneOf, value) {
			return fmt.Errorf("%s must be one of: %s", entry.Key, strings.Join(entry.OneOf, ", "))
		}
	}
	return nil
}

func slicesContains(list []string, want string) bool {
	return slices.Contains(list, want)
}

// variable is the wire shape of one setting.
type variable struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	IsPublic bool   `json:"is_public,omitzero"`
}

// mergedValues folds DB overrides over env-provided defaults, which
// in turn fold over the catalog defaults: catalog < env < DB.
func mergedValues(envDefaults, overrides map[string]string) map[string]string {
	out := make(map[string]string, len(configKeys))
	for _, entry := range configKeys {
		out[entry.Key] = entry.Default
	}
	for key, value := range envDefaults {
		if _, known := lookup(key); known {
			out[key] = value
		}
	}
	for key, value := range overrides {
		if _, known := lookup(key); known {
			out[key] = value
		}
	}
	return out
}

// publicView lists the keys flagged public (the unauthenticated SPA
// bootstrap payload).
func publicView(values map[string]string) []variable {
	out := make([]variable, 0, len(configKeys))
	for _, entry := range configKeys {
		if !entry.Public {
			continue
		}
		out = append(out, variable{Key: entry.Key, Type: string(entry.Type), Value: values[entry.Key]})
	}
	return out
}

// allView lists every key with its visibility flag (admin). The
// stored value of a sensitive key never leaves the server: the
// response carries an empty string instead.
func allView(values map[string]string) []variable {
	out := make([]variable, 0, len(configKeys))
	for _, entry := range configKeys {
		value := values[entry.Key]
		if entry.Sensitive {
			value = ""
		}
		out = append(out, variable{
			Key:      entry.Key,
			Type:     string(entry.Type),
			Value:    value,
			IsPublic: entry.Public,
		})
	}
	return out
}
