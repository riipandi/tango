// Package mailer sends the application's email over SMTP.
//
// The mailer is optional. An empty mailer.smtp_host means the mailer is built
// and refuses to send, which is what keeps a local checkout from needing a mail
// server; nothing else in the process changes.
//
// One *Mailer is built in internal/registry and shared by the process. Templates
// are a second value: *Templates renders the React Email output embedded in the
// binary, so a feature asks for the message it wants rather than assembling MIME
// itself.
package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-sasl"
	"github.com/emersion/go-smtp"

	"github.com/riipandi/tango/internal/config"
)

// Message is one email to send. HTML and Text are alternative bodies of the
// same message; at least one is required. Bcc is delivered to but never written
// into the headers, so it is not visible to the recipients.
type Message struct {
	To      []string
	Cc      []string
	Bcc     []string
	Subject string
	HTML    string
	Text    string
	// Headers carries extra header lines such as Reply-To or List-Unsubscribe.
	// A name or value holding a line break is refused: a header is one line.
	Headers map[string]string
}

// Mailer submits messages to the configured SMTP server. It is safe for
// concurrent use; every Send opens and closes its own session.
type Mailer struct {
	settings config.Mailer
	log      *slog.Logger
}

// New builds the mailer the configuration describes.
//
// A missing smtp_host is not an error: the mailer is then unconfigured and every
// Send reports ErrNotConfigured. log may be nil, which discards the lines a test
// is not reading.
func New(cfg config.Config, log *slog.Logger) (*Mailer, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if cfg.Mailer.SMTPHost != "" && cfg.Mailer.SMTPPort <= 0 {
		return nil, errors.New("mailer: smtp_port must be set when smtp_host is")
	}
	return &Mailer{settings: cfg.Mailer, log: log}, nil
}

// Configured reports whether a mail server is named. A command that sends mail
// checks this rather than treating ErrNotConfigured as a failure of its own.
func (m *Mailer) Configured() bool {
	return m != nil && m.settings.SMTPHost != ""
}

// Address is the host:port every message is submitted to. It is the mailer's
// only published target, and it never carries a credential.
func (m *Mailer) Address() string {
	if m == nil {
		return ""
	}
	return net.JoinHostPort(m.settings.SMTPHost, strconv.Itoa(m.settings.SMTPPort))
}

// Send submits one message and waits for the server's answer.
//
// The caller's context ends the session: a cancellation closes the connection,
// so a blocked read fails instead of waiting out the server. An error is
// classified and matched with errors.Is: ErrNetwork, ErrTimeout, ErrCanceled,
// ErrAuth, ErrRejected, ErrTemporary, or ErrNotConfigured.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if !m.Configured() {
		return ErrNotConfigured
	}
	if ctx == nil {
		ctx = context.Background()
	}

	envelope, err := m.envelope(msg)
	if err != nil {
		return err
	}

	client, err := m.connect(ctx)
	if err != nil {
		return err
	}
	// The session is closed on every path: QUIT is a courtesy, and a failure
	// before it must still release the connection. Closing twice is harmless.
	defer client.Close()
	// A blocked read has no other way out. The caller's context is what ends
	// the session, so giving up does not wait out the server's own timeout.
	stop := context.AfterFunc(ctx, func() { _ = client.Close() })
	defer stop()

	started := time.Now()
	if err := m.authenticate(client); err != nil {
		return classify(OpAuth, m.Address(), err)
	}
	if err := client.SendMail(envelope.from, envelope.rcpt, bytes.NewReader(envelope.raw)); err != nil {
		return classify(OpSend, m.Address(), err)
	}

	// The submission is accepted at this point: a QUIT that fails says nothing
	// about the message, so it is not reported as a send failure.
	if err := client.Quit(); err != nil {
		m.log.DebugContext(ctx, "mailer: quit", "target", m.Address(), "err", err.Error())
	}
	m.log.DebugContext(ctx, "mailer: sent",
		"target", m.Address(),
		"recipients", len(envelope.rcpt),
		"duration", time.Since(started).String())
	return nil
}

// connect opens a session and completes the greeting.
//
// The session is encrypted when the configuration asks for it, and upgraded with
// STARTTLS when the server offers it. A server that offers neither is submitted
// to in the clear — the local development server is one — and that is reported,
// so a deployment that believes it is encrypted hears otherwise.
func (m *Mailer) connect(ctx context.Context) (*smtp.Client, error) {
	if m.settings.SMTPSecure {
		conn, err := m.dialTLS(ctx)
		if err != nil {
			return nil, classify(OpDial, m.Address(), err)
		}
		return m.greet(smtp.NewClient(conn)), nil
	}

	conn, err := m.dial(ctx)
	if err != nil {
		return nil, classify(OpDial, m.Address(), err)
	}
	client, err := smtp.NewClientStartTLS(conn, m.tlsConfig())
	switch {
	case err == nil:
		// The STARTTLS handshake sends the greeting itself, so this session
		// introduces itself with the client's default name. Every server that
		// offers STARTTLS accepts it.
		return m.limit(client), nil
	case isStartTLSUnsupported(err):
		// The failed attempt closed its own connection, so the plaintext
		// session is a second dial. Only a server without TLS pays for it.
		m.log.WarnContext(ctx, "mailer: session is not encrypted",
			"target", m.Address(),
			"hint", "the server offers no STARTTLS; set mailer.smtp_secure for implicit TLS")
		plain, dialErr := m.dial(ctx)
		if dialErr != nil {
			return nil, classify(OpDial, m.Address(), dialErr)
		}
		return m.greet(smtp.NewClient(plain)), nil
	default:
		return nil, classify(OpDial, m.Address(), err)
	}
}

// startTLSUnsupported is the message smtp.NewClientStartTLS returns when the
// server does not advertise STARTTLS. The library exports no sentinel for it, so
// the text is what identifies it; TestStartTLSUnsupportedMatchesTheLibrary pins
// the message against a server that does not offer the extension, so a library
// change fails a test rather than silently moving a session to plaintext.
const startTLSUnsupported = "smtp: server doesn't support STARTTLS"

// isStartTLSUnsupported reports whether err is the library's "no STARTTLS here"
// answer.
func isStartTLSUnsupported(err error) bool {
	return err != nil && err.Error() == startTLSUnsupported
}

// greet introduces the session before any command is sent, then bounds it. The
// name is the sender's domain, which is the part a receiving server can resolve
// back; the client's own default is "localhost".
func (m *Mailer) greet(client *smtp.Client) *smtp.Client {
	client = m.limit(client)
	if err := client.Hello(localName(m.settings.FromEmail)); err != nil {
		// The client keeps the failure and returns it from the command that
		// needed the greeting, where it is classified like any other.
		m.log.Debug("mailer: greeting", "target", m.Address(), "err", err.Error())
	}
	return client
}

// dial opens a plaintext connection to the server, bounded by the attempt
// timeout and the caller's context.
func (m *Mailer) dial(ctx context.Context) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: m.settings.Timeout}
	return dialer.DialContext(ctx, "tcp", m.Address())
}

// dialTLS opens an implicitly encrypted connection, which is what port 465
// serves.
func (m *Mailer) dialTLS(ctx context.Context) (net.Conn, error) {
	dialer := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: m.settings.Timeout},
		Config:    m.tlsConfig(),
	}
	return dialer.DialContext(ctx, "tcp", m.Address())
}

// tlsConfig verifies the server certificate against the host it was reached by,
// so a certificate issued for another name is refused rather than accepted
// quietly.
func (m *Mailer) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName: m.settings.SMTPHost,
		MinVersion: tls.VersionTLS12,
	}
}

// limit bounds each command and the message body. The library's own defaults are
// RFC 5321's (five minutes a command, twelve a submission), which is far longer
// than a submission this process makes should ever take.
func (m *Mailer) limit(client *smtp.Client) *smtp.Client {
	client.CommandTimeout = m.settings.Timeout
	client.SubmissionTimeout = m.settings.Timeout
	return client
}

// authenticate logs in when credentials are configured. PLAIN is preferred;
// LOGIN is used where the server offers only that.
func (m *Mailer) authenticate(client *smtp.Client) error {
	if m.settings.SMTPUsername == "" {
		return nil
	}
	user, pass := m.settings.SMTPUsername, m.settings.SMTPPassword
	switch {
	case client.SupportsAuth(sasl.Plain):
		return client.Auth(sasl.NewPlainClient("", user, pass))
	case client.SupportsAuth(sasl.Login):
		return client.Auth(sasl.NewLoginClient(user, pass))
	default:
		return errNoAuthMechanism
	}
}

// localName is the host the greeting introduces: the sender's domain, which is
// the name a receiving server can check, or "localhost" when the address carries
// none.
func localName(from string) string {
	if _, domain, ok := strings.Cut(from, "@"); ok && domain != "" {
		return domain
	}
	return "localhost"
}

// errNoAuthMechanism is returned when credentials are configured and the server
// offers no way to present them.
var errNoAuthMechanism = errors.New("server offers no supported authentication mechanism")

// String renders the mailer without a credential: the address and whether a
// session is authenticated, never the username or the password.
func (m *Mailer) String() string {
	if m == nil || !m.Configured() {
		return "mailer: not configured"
	}
	return fmt.Sprintf("mailer: %s (auth %t)", m.Address(), m.settings.SMTPUsername != "")
}
