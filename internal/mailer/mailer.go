package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"go.loglayer.dev/v3"
)

// smtpMailer implements Mailer over an SMTP relay.
type smtpMailer struct {
	cfg         config.MailerConfig
	store       *templateStore
	log         logger.Logger
	sendTimeout time.Duration
}

// New builds the mailer from the app's mailer config section.
// Connections open per send and close after delivery — the relay
// is shared infrastructure, not a per-app resource.
func New(cfg config.MailerConfig, opts Options) Mailer {
	if opts.Logger == nil {
		opts.Logger = loglayer.NewMock()
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = DefaultSendTimeout
	}
	return &smtpMailer{
		cfg:         cfg,
		store:       newTemplateStore(opts.Templates, opts.LogoURL),
		log:         opts.Logger,
		sendTimeout: opts.SendTimeout,
	}
}

// Send renders the template and delivers the message over SMTP.
func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return errors.New("mailer: recipient is empty")
	}
	if msg.Template == "" {
		return errors.New("mailer: template is empty")
	}

	htmlBody, textBody, err := m.store.render(msg.Template, msg.To, msg.Data)
	if err != nil {
		return err
	}

	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mailer: invalid recipient: %w", err)
	}

	from := mail.Address{Name: m.cfg.FromName, Address: m.cfg.FromEmail}
	payload, err := m.compose(&from, to, msg.Subject, textBody, htmlBody)
	if err != nil {
		return err
	}

	fields := loglayer.M{"to": to.Address, "template": msg.Template}

	m.log.WithMetadata(fields).Debug("sending email")

	if err := m.deliver(ctx, &from, to.Address, payload); err != nil {
		m.log.WithMetadata(fields).WithError(err).Error("email delivery failed")
		return err
	}

	m.log.WithMetadata(fields).Info("email sent")
	return nil
}

// compose builds the RFC 5322 message: headers plus a
// multipart/alternative body (plain text first, HTML second).
func (m *smtpMailer) compose(from, to *mail.Address, subject, textBody, htmlBody string) ([]byte, error) {
	buf := &bytes.Buffer{}
	writer := multipart.NewWriter(buf)

	headers := strings.Join([]string{
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + mime.QEncoding.Encode("utf-8", subject),
		"MIME-Version: 1.0",
		"Content-Type: multipart/alternative; boundary=" + writer.Boundary(),
	}, "\r\n") + "\r\n\r\n"
	buf.WriteString(headers)

	textPart, err := writer.CreatePart(mimePart("text/plain; charset=utf-8"))
	if err != nil {
		return nil, fmt.Errorf("mailer: text part: %w", err)
	}
	if _, werr := textPart.Write([]byte(textBody)); werr != nil {
		return nil, fmt.Errorf("mailer: text part: %w", werr)
	}

	htmlPart, err := writer.CreatePart(mimePart("text/html; charset=utf-8"))
	if err != nil {
		return nil, fmt.Errorf("mailer: html part: %w", err)
	}
	if _, werr := htmlPart.Write([]byte(htmlBody)); werr != nil {
		return nil, fmt.Errorf("mailer: html part: %w", werr)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("mailer: close body: %w", err)
	}
	return buf.Bytes(), nil
}

// deliver runs the SMTP transaction against the configured relay.
func (m *smtpMailer) deliver(ctx context.Context, from *mail.Address, to string, payload []byte) error {
	addr := fmt.Sprintf("%s:%d", m.cfg.SMTPHost, m.cfg.SMTPPort)

	client, err := m.dial(addr)
	if err != nil {
		return err
	}
	defer client.Close() //nolint:errcheck

	// Bounds for command responses and the final-dot handoff; ctx
	// deadlines apply at the next reload of this API.
	client.CommandTimeout = m.sendTimeout
	client.SubmissionTimeout = m.sendTimeout

	if m.cfg.SMTPUsername != "" || m.cfg.SMTPPassword != "" {
		if err = client.Auth(sasl.NewPlainClient("", m.cfg.SMTPUsername, m.cfg.SMTPPassword)); err != nil {
			return fmt.Errorf("mailer: auth: %w", err)
		}
	}

	if err = client.Mail(from.Address, nil); err != nil {
		return fmt.Errorf("mailer: mail from: %w", err)
	}
	if err = client.Rcpt(to, nil); err != nil {
		return fmt.Errorf("mailer: rcpt: %w", err)
	}

	data, err := client.Data()
	if err != nil {
		return fmt.Errorf("mailer: data: %w", err)
	}
	if _, werr := data.Write(payload); werr != nil {
		data.Close() //nolint:errcheck
		return fmt.Errorf("mailer: write payload: %w", werr)
	}
	if err = data.Close(); err != nil {
		return fmt.Errorf("mailer: deliver payload: %w", err)
	}

	if err = client.Quit(); err != nil {
		return fmt.Errorf("mailer: quit: %w", err)
	}
	return nil
}

// dial opens the connection: implicit TLS when the config marks the
// relay secure, otherwise plaintext with opportunistic STARTTLS
// (upgraded before authentication when the relay supports it).
func (m *smtpMailer) dial(addr string) (*gosmtp.Client, error) {
	tlsConfig := &tls.Config{ServerName: m.cfg.SMTPHost}

	if m.cfg.SMTPSecure {
		client, err := gosmtp.DialTLS(addr, tlsConfig)
		if err != nil {
			return nil, fmt.Errorf("mailer: dial tls: %w", err)
		}
		return client, nil
	}

	client, err := gosmtp.Dial(addr)
	if err != nil {
		return nil, fmt.Errorf("mailer: dial: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); !ok {
		return client, nil
	}

	client.Close() //nolint:errcheck
	upgraded, err := gosmtp.DialStartTLS(addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("mailer: starttls: %w", err)
	}
	return upgraded, nil
}

func mimePart(contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{"Content-Type": {contentType}}
}
