// Package mailer renders templates and sends transactional email over SMTP.
package mailer

import (
	"context"
	"io/fs"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
)

// Mailer renders and delivers transactional email.
type Mailer interface {
	Send(ctx context.Context, msg Message) error
}

// SettingsSource resolves SMTP settings for one send.
type SettingsSource func(ctx context.Context) (config.MailerConfig, error)

// SettingsSourceSetter is implemented by mailers that accept a settings source.
type SettingsSourceSetter interface {
	SetSettingsSource(source SettingsSource)
}

// Message is a message to render and send.
type Message struct {
	// To is the recipient address.
	To string

	// Subject is the message subject.
	Subject string

	// Template names resolve to matching HTML and text templates.
	Template string

	// Data feeds the template's .Data tree.
	Data map[string]any
}

// Options configures New.
type Options struct {
	// Templates is the source of template files.
	Templates fs.FS

	// LogoURL is available to templates.
	LogoURL string

	// Logger receives send attempts and failures. Nil disables logging.
	Logger logger.Logger

	// SendTimeout bounds one SMTP transaction when ctx has no
	// deadline. Zero uses DefaultSendTimeout.
	SendTimeout time.Duration
}

// DefaultSendTimeout bounds one SMTP transaction.
const DefaultSendTimeout = 15 * time.Second
