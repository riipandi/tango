// Package mailer delivers transactional email: React Email templates
// (compiled to Go templates, embedded via web.EmailTemplates)
// rendered with html/template for HTML and text/template for plain
// text, sent over SMTP (emersion/go-smtp) with SASL auth.
//
// Rendered templates are cached in memory — the embedded FS is
// immutable, so each template parses exactly once per process.
package mailer

import (
	"context"
	"io/fs"
	"time"

	"github.com/riipandi/tango/internal/logger"
)

// Mailer renders and delivers transactional email.
type Mailer interface {
	// Send renders the requested template and delivers the message.
	Send(ctx context.Context, msg Message) error
}

// Message is a render-and-send request.
type Message struct {
	// To is the recipient email address.
	To string

	// Subject is the plain subject line (encoded for non-ASCII).
	Subject string

	// Template is the template name, e.g. "email-verification" —
	// resolves to <name>_html.tmpl and <name>_text.tmpl.
	Template string

	// Data feeds the template's .Data tree (e.g. VerificationLink).
	Data map[string]any
}

// Options parametrizes New.
type Options struct {
	// Templates is the source of *.tmpl files. Required.
	Templates fs.FS

	// LogoURL feeds the templates' .LogoURL header (optional).
	LogoURL string

	// Logger receives send attempts and failures. Nil stays silent.
	Logger logger.Logger

	// SendTimeout bounds a single SMTP transaction when the context
	// carries no deadline. Zero uses DefaultSendTimeout.
	SendTimeout time.Duration
}

// DefaultSendTimeout bounds a single SMTP transaction.
const DefaultSendTimeout = 15 * time.Second
