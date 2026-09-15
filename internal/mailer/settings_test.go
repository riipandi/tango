package mailer

import (
	"testing"

	"github.com/riipandi/tango/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestSettingsFromValues(t *testing.T) {
	fallback := config.MailerConfig{
		FromEmail: "mailer@example.com", FromName: "Env Name",
		SMTPHost: "env-host", SMTPPort: 1025,
	}

	// Empty values keep the env fallback per field.
	values := map[string]string{}
	got := SettingsFromValues(values, fallback)
	assert.Equal(t, "env-host", got.SMTPHost)
	assert.Equal(t, 1025, got.SMTPPort)

	// DB overrides win, port parses, secure flips.
	got = SettingsFromValues(map[string]string{
		"smtp_host": "db-host", "smtp_port": "2525", "smtp_secure": "true",
		"smtp_from_name": "DB Name",
	}, fallback)
	assert.Equal(t, "db-host", got.SMTPHost)
	assert.Equal(t, 2525, got.SMTPPort)
	assert.True(t, got.SMTPSecure)
	assert.Equal(t, "DB Name", got.FromName)

	// A bad port string is ignored, not fatal.
	got = SettingsFromValues(map[string]string{"smtp_port": "soon"}, fallback)
	assert.Equal(t, 1025, got.SMTPPort)
}
