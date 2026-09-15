package appconfig

import (
	"strconv"

	jsonv2 "encoding/json/v2"

	"github.com/riipandi/tango/internal/config"
)

// env.go maps internal/config sections onto the admin-editable keys:
// the env layer of the defaults fold, and parsed projections of the
// merged values for cross-module consumers.

// EnvDefaults maps the koanf Mailer section onto the appconfig
// keys as the env layer of the defaults fold.
func EnvDefaults(cfg *config.Config) map[string]string {
	return map[string]string{
		"smtp_from_email": cfg.Mailer.FromEmail,
		"smtp_from_name":  cfg.Mailer.FromName,
		"smtp_host":       cfg.Mailer.SMTPHost,
		"smtp_port":       strconv.Itoa(cfg.Mailer.SMTPPort),
		"smtp_username":   cfg.Mailer.SMTPUsername,
		"smtp_password":   cfg.Mailer.SMTPPassword,
		"smtp_secure":     strconv.FormatBool(cfg.Mailer.SMTPSecure),
	}
}

// CIMDAllowlist reads the operator-managed CIMD URL allowlist from
// the merged values (JSON array; default deny when unset).
func CIMDAllowlist(values map[string]string) []string {
	var allowlist []string
	_ = jsonv2.Unmarshal([]byte(values["cimd_url_allowlist"]), &allowlist)
	return allowlist
}
