// Package mailer sends transactional email: React Email templates
// (embedded) rendered dual HTML/text, delivered over SMTP with
// SASL auth. Parsed templates cache in memory; FS is immutable.
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

// SettingsSource resolves the relay settings per send: the appconfig
// surface owns the SMTP keys once wired (env values are its default
// layer). Returning an error falls back to the static config.
type SettingsSource func(ctx context.Context) (config.MailerConfig, error)

// SettingsSourceSetter is implemented by mailers that accept a
// late-bound settings source (the appconfig module exists after the
// mailer is constructed).
type SettingsSourceSetter interface {
	SetSettingsSource(source SettingsSource)
}

// Message is a render-and-send request.
type Message struct {
	// To is the recipient address.
	To string

	// Subject is the plain subject (non-ASCII encoded).
	Subject string

	// Template name, e.g. "email-verification" resolves to
	// <name>_html.tmpl and <name>_text.tmpl.
	Template string

	// Data feeds the template's .Data tree.
	Data map[string]any
}

// Options parametrizes New.
type Options struct {
	// Templates source of *.tmpl files. Required.
	Templates fs.FS

	// LogoURL feeds templates' .LogoURL header.
	LogoURL string

	// Logger gets send attempts/failures. Nil silences.
	Logger logger.Logger

	// SendTimeout bounds one SMTP transaction when ctx has no
	// deadline. Zero uses DefaultSendTimeout.
	SendTimeout time.Duration
}

// DefaultSendTimeout bounds one SMTP transaction.
const DefaultSendTimeout = 15 * time.Second
