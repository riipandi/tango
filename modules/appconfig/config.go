package appconfig

// config.go catalogs the admin-editable settings: key, value type,
// public visibility, and the env-backed default. Upstream reference:
// internal/appconfig/model.go (tag-driven reflect walk) — tango uses
// an explicit table so grep finds every key. Trimmed set: SMTP and
// LDAP stay env-only (tango deviation); sensitive keys do not exist
// here, so /all needs no redaction pass.

import (
	"fmt"
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

// signupModes mirrors the upstream allow_user_signups enum.
var signupModes = []string{"disabled", "withToken", "open"}

// webauthnVerifications mirrors the upstream user-verification enum.
var webauthnVerifications = []string{"required", "preferred"}

// webauthnAttachments mirrors the upstream attachment enum.
var webauthnAttachments = []string{"any", "platform", "cross-platform"}

// configKey describes one editable setting.
type configKey struct {
	Key     string
	Type    valueType
	Public  bool
	Default string
	// OneOf restricts string values to a fixed set (empty = free).
	OneOf []string
}

// configKeys is the full catalog; keep Default in sync with
// internal/config + .env.example.
var configKeys = []configKey{
	// General
	{Key: "app_name", Type: typeString, Public: true, Default: "tango"},
	{Key: "session_duration", Type: typeInt, Default: "43200"}, // minutes; 30 days
	{Key: "home_page_url", Type: typeString, Public: true, Default: "/"},
	{Key: "accent_color", Type: typeString, Public: true},
	{Key: "disable_animations", Type: typeBool, Public: true},
	{Key: "allow_own_account_edit", Type: typeBool, Public: true, Default: "true"},
	{Key: "allow_user_signups", Type: typeString, Public: true, Default: "disabled", OneOf: signupModes},

	// Signup defaults (JSON-encoded, like upstream)
	{Key: "signup_default_user_group_ids", Type: typeString},
	{Key: "signup_default_custom_claims", Type: typeString},

	// Email policy (SMTP credentials stay env-only)
	{Key: "require_user_email", Type: typeBool, Public: true},
	{Key: "email_login_notification_enabled", Type: typeBool, Default: "true"},
	{Key: "email_one_time_access_as_unauthenticated_enabled", Type: typeBool, Public: true},
	{Key: "email_one_time_access_as_admin_enabled", Type: typeBool, Public: true, Default: "true"},
	{Key: "email_api_key_expiration_enabled", Type: typeBool, Default: "true"},
	{Key: "email_verification_enabled", Type: typeBool, Public: true},

	// WebAuthn ceremony tuning
	{Key: "webauthn_user_verification", Type: typeString, Default: "preferred", OneOf: webauthnVerifications},
	{Key: "webauthn_allow_synced_passkeys", Type: typeBool, Default: "true"},
	{Key: "webauthn_authenticator_attachment", Type: typeString, Default: "any", OneOf: webauthnAttachments},

	// OIDC
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
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// variable is the wire shape of one setting (snake_case; upstream
// sends camelCase {key,type,value}).
type variable struct {
	Key      string `json:"key"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	IsPublic bool   `json:"is_public,omitzero"`
}

// mergedValues folds DB overrides over env/env-file defaults.
func mergedValues(overrides map[string]string) map[string]string {
	out := make(map[string]string, len(configKeys))
	for _, entry := range configKeys {
		out[entry.Key] = entry.Default
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

// allView lists every key with its visibility flag (admin).
func allView(values map[string]string) []variable {
	out := make([]variable, 0, len(configKeys))
	for _, entry := range configKeys {
		out = append(out, variable{
			Key:      entry.Key,
			Type:     string(entry.Type),
			Value:    values[entry.Key],
			IsPublic: entry.Public,
		})
	}
	return out
}
