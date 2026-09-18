package appconfig

// config.go defines the admin-editable settings and their defaults.

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	jsonv2 "encoding/json/v2"
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
	// ValidateJSON, when set, checks the stored string form against
	// the named JSON shape (the reader of this key parses it).
	ValidateJSON func(value string) error
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

	// WebAuthn policy
	{Key: "webauthn_user_verification", Type: typeString, Default: "preferred", OneOf: webauthnVerifications},
	{Key: "webauthn_allow_synced_passkeys", Type: typeBool, Default: "true"},
	{Key: "webauthn_authenticator_attachment", Type: typeString, Default: "any", OneOf: webauthnAttachments},

	// CIMD allowlist is a JSON array of URL prefixes; the reader
	// fails closed on a malformed value, so writes validate the JSON.
	{Key: "cimd_url_allowlist", Type: typeString, ValidateJSON: jsonArrayOfStrings},
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
		if len(entry.OneOf) > 0 && value != "" && !slices.Contains(entry.OneOf, value) {
			return fmt.Errorf("%s must be one of: %s", entry.Key, strings.Join(entry.OneOf, ", "))
		}
		if entry.ValidateJSON != nil && value != "" {
			if err := entry.ValidateJSON(value); err != nil {
				return fmt.Errorf("%s is invalid: %w", entry.Key, err)
			}
		}
	}
	return nil
}

// jsonArrayOfStrings requires a JSON array of strings.
func jsonArrayOfStrings(value string) error {
	var items []string
	if err := jsonv2.Unmarshal([]byte(value), &items); err != nil {
		return fmt.Errorf("must be a JSON array of strings")
	}
	return nil
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
