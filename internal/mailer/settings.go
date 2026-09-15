package mailer

import (
	"strconv"

	"github.com/riipandi/tango/internal/config"
)

// SettingsFromValues merges app settings over the fallback config.
func SettingsFromValues(values map[string]string, fallback config.MailerConfig) config.MailerConfig {
	out := fallback
	if v := values["smtp_from_email"]; v != "" {
		out.FromEmail = v
	}
	if v := values["smtp_from_name"]; v != "" {
		out.FromName = v
	}
	if v := values["smtp_host"]; v != "" {
		out.SMTPHost = v
	}
	if v := values["smtp_port"]; v != "" {
		if port, err := strconv.Atoi(v); err == nil {
			out.SMTPPort = port
		}
	}
	if v, ok := values["smtp_username"]; ok {
		out.SMTPUsername = v
	}
	if v, ok := values["smtp_password"]; ok {
		out.SMTPPassword = v
	}
	if v := values["smtp_secure"]; v != "" {
		out.SMTPSecure = v == "true"
	}
	return out
}
