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

// smtpMailer implements Mailer with SMTP.
type smtpMailer struct {
	cfg         config.MailerConfig
	source      SettingsSource
	store       *templateStore
	log         logger.Logger
	sendTimeout time.Duration
}

// New builds a mailer from SMTP settings. Every compiled template
// is parsed eagerly so a broken template fails the boot, not the
// first send.
func New(cfg config.MailerConfig, opts Options) Mailer {
	if opts.Logger == nil {
		opts.Logger = loglayer.NewMock()
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = DefaultSendTimeout
	}
	store := newTemplateStore(opts.Templates, opts.LogoURL)
	if err := store.Warm(); err != nil {
		panic("mailer: " + err.Error())
	}
	return &smtpMailer{
		cfg:         cfg,
		store:       store,
		log:         opts.Logger,
		sendTimeout: opts.SendTimeout,
	}
}

// SetSettingsSource sets the per-send settings resolver.
func (m *smtpMailer) SetSettingsSource(source SettingsSource) {
	m.source = source
}

// settings resolves SMTP settings for one send.
func (m *smtpMailer) settings(ctx context.Context) config.MailerConfig {
	if m.source == nil {
		return m.cfg
	}
	resolved, err := m.source(ctx)
	if err != nil || (resolved.SMTPHost == "" && resolved.SMTPPort == 0) {
		if err != nil {
			m.log.Warn("mailer: settings source failed, falling back to env config", loglayer.M{"error": err.Error()})
		}
		return m.cfg
	}
	return resolved
}

// Send renders and delivers one message over SMTP.
func (m *smtpMailer) Send(ctx context.Context, msg Message) error {
	if msg.To == "" {
		return errors.New("mailer: recipient is empty")
	}
	if msg.Template == "" {
		return errors.New("mailer: template is empty")
	}

	cfg := m.settings(ctx)

	htmlBody, textBody, err := m.store.render(msg.Template, msg.To, msg.Data)
	if err != nil {
		return err
	}

	to, err := mail.ParseAddress(msg.To)
	if err != nil {
		return fmt.Errorf("mailer: invalid recipient: %w", err)
	}

	from := mail.Address{Name: cfg.FromName, Address: cfg.FromEmail}
	payload, err := m.compose(&from, to, msg.Subject, textBody, htmlBody)
	if err != nil {
		return err
	}

	fields := loglayer.M{"to": to.Address, "template": msg.Template}

	m.log.WithMetadata(fields).Debug("sending email")

	if err := m.deliver(ctx, cfg, &from, to.Address, payload); err != nil {
		m.log.WithMetadata(fields).WithError(err).Error("email delivery failed")
		return err
	}

	m.log.WithMetadata(fields).Info("email sent")
	return nil
}

// compose builds a multipart/alternative message.
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

// deliver runs the SMTP transaction.
func (m *smtpMailer) deliver(ctx context.Context, cfg config.MailerConfig, from *mail.Address, to string, payload []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.SMTPHost, cfg.SMTPPort)

	client, err := m.dial(addr, cfg)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	client.CommandTimeout = m.sendTimeout
	client.SubmissionTimeout = m.sendTimeout

	if cfg.SMTPUsername != "" || cfg.SMTPPassword != "" {
		if err = client.Auth(sasl.NewPlainClient("", cfg.SMTPUsername, cfg.SMTPPassword)); err != nil {
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
		_ = data.Close()
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

// dial connects with implicit TLS or STARTTLS.
func (m *smtpMailer) dial(addr string, cfg config.MailerConfig) (*gosmtp.Client, error) {
	tlsConfig := &tls.Config{ServerName: cfg.SMTPHost}

	if cfg.SMTPSecure {
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

	_ = client.Close()
	upgraded, err := gosmtp.DialStartTLS(addr, tlsConfig)
	if err != nil {
		return nil, fmt.Errorf("mailer: starttls: %w", err)
	}
	return upgraded, nil
}

func mimePart(contentType string) textproto.MIMEHeader {
	return textproto.MIMEHeader{"Content-Type": {contentType}}
}
