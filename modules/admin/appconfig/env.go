package appconfig

import (
	jsonv2 "encoding/json/v2"
)

// env.go exposes parsed projections of the merged settings for
// cross-module consumers. Environment-backed settings (SMTP relay
// credentials, lifetimes, secrets) never enter this catalog: their
// single source is the environment.

// CIMDAllowlist reads the operator-managed CIMD URL allowlist from
// the merged values (JSON array; default deny when unset).
func CIMDAllowlist(values map[string]string) []string {
	var allowlist []string
	_ = jsonv2.Unmarshal([]byte(values["cimd_url_allowlist"]), &allowlist)
	return allowlist
}
