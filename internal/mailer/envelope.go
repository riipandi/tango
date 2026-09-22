package mailer

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/textproto"
	"slices"
	"strings"
	"time"
)

// envelope is a message resolved into what the SMTP session needs: the sender,
// every recipient including the blind ones, and the RFC 5322 bytes.
type envelope struct {
	from string
	rcpt []string
	raw  []byte
}

// errNoRecipient is returned for a message addressed to nobody. A submission
// with no recipient is refused by the server anyway; catching it here names the
// mistake at the caller.
var errNoRecipient = errors.New("mailer: message has no recipient")

// envelope renders msg as an RFC 5322 message.
//
// A message with both bodies is multipart/alternative, so a client picks the
// HTML and a text-only reader still gets the plain one. A message with one body
// is sent as that body alone: a multipart wrapper around a single part is
// pointless, and some readers show the wrapper's boilerplate.
func (m *Mailer) envelope(msg Message) (*envelope, error) {
	if err := msg.validate(); err != nil {
		return nil, err
	}

	rcpt := make([]string, 0, len(msg.To)+len(msg.Cc)+len(msg.Bcc))
	rcpt = append(rcpt, msg.To...)
	rcpt = append(rcpt, msg.Cc...)
	rcpt = append(rcpt, msg.Bcc...)
	if len(rcpt) == 0 {
		return nil, errNoRecipient
	}

	boundary, err := newBoundary()
	if err != nil {
		return nil, err
	}

	var body bytes.Buffer
	header := textproto.MIMEHeader{}
	header.Set("From", m.fromHeader())
	// A message may carry only a Cc: a To header with no address in it is worse
	// than no To header.
	if len(msg.To) > 0 {
		header.Set("To", strings.Join(msg.To, ", "))
	}
	if len(msg.Cc) > 0 {
		header.Set("Cc", strings.Join(msg.Cc, ", "))
	}
	// Bcc is deliberately absent: it is an envelope recipient only, which is
	// what keeps it blind.
	header.Set("Subject", mime.QEncoding.Encode("utf-8", msg.Subject))
	header.Set("Date", time.Now().Format(time.RFC1123Z))
	header.Set("Message-ID", messageID(m.settings.FromEmail))
	header.Set("MIME-Version", "1.0")
	for name, value := range msg.Headers {
		header.Set(name, value)
	}

	switch {
	case msg.HTML != "" && msg.Text != "":
		header.Set("Content-Type", "multipart/alternative; boundary="+boundary)
		if err := writeAlternative(&body, boundary, msg); err != nil {
			return nil, err
		}
	case msg.HTML != "":
		writeSingle(&body, header, "text/html", msg.HTML)
	default:
		writeSingle(&body, header, "text/plain", msg.Text)
	}

	var raw bytes.Buffer
	writeHeader(&raw, header)
	raw.WriteString("\r\n")
	raw.Write(body.Bytes())

	return &envelope{from: m.settings.FromEmail, rcpt: rcpt, raw: raw.Bytes()}, nil
}

// validate refuses a message the SMTP session cannot represent: no subject, no
// body, an address with a line break, or a header that would inject one.
func (msg Message) validate() error {
	switch {
	case msg.Subject == "":
		return errors.New("mailer: message has no subject")
	case msg.HTML == "" && msg.Text == "":
		return errors.New("mailer: message has no body")
	}
	for _, addr := range append(append(append([]string{}, msg.To...), msg.Cc...), msg.Bcc...) {
		if err := checkAddress(addr); err != nil {
			return err
		}
	}
	for name, value := range msg.Headers {
		if err := checkHeader(name, value); err != nil {
			return err
		}
	}
	return nil
}

// checkAddress refuses an address that would break the SMTP command or inject a
// header. The envelope is written line by line, so a line break in an address is
// the whole attack.
func checkAddress(addr string) error {
	switch {
	case strings.TrimSpace(addr) == "":
		return errors.New("mailer: recipient must not be empty")
	case strings.ContainsAny(addr, "\r\n"):
		return errors.New("mailer: recipient must not contain a line break")
	case strings.ContainsAny(addr, "<>,"):
		return fmt.Errorf("mailer: recipient %q must be a bare address", addr)
	case !strings.Contains(addr, "@"):
		return fmt.Errorf("mailer: recipient %q is not an email address", addr)
	default:
		return nil
	}
}

// checkHeader refuses a header name or value holding a line break, which is how
// a caller-supplied header would add one of its own.
func checkHeader(name, value string) error {
	switch {
	case strings.TrimSpace(name) == "":
		return errors.New("mailer: header name must not be empty")
	case strings.ContainsAny(name, "\r\n: "):
		return fmt.Errorf("mailer: header name %q is not a field name", name)
	case strings.ContainsAny(value, "\r\n"):
		return fmt.Errorf("mailer: header %s must not contain a line break", name)
	default:
		return nil
	}
}

// fromHeader renders the sender as the display name and the address.
func (m *Mailer) fromHeader() string {
	if m.settings.FromName == "" {
		return m.settings.FromEmail
	}
	return mime.QEncoding.Encode("utf-8", m.settings.FromName) + " <" + m.settings.FromEmail + ">"
}

// writeSingle writes one body as the message's only part, encoding it as
// quoted-printable so a long HTML line survives a transport that rewraps.
func writeSingle(body *bytes.Buffer, header textproto.MIMEHeader, contentType, content string) {
	header.Set("Content-Type", contentType+"; charset=utf-8")
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	w := quotedprintable.NewWriter(body)
	_, _ = w.Write([]byte(content))
	_ = w.Close()
}

// writeAlternative writes the two bodies as alternatives in one multipart part.
// The text part comes first: a client that does not understand the HTML shows
// the last part it can, and every client understands text/plain.
func writeAlternative(body *bytes.Buffer, boundary string, msg Message) error {
	w := multipart.NewWriter(body)
	if err := w.SetBoundary(boundary); err != nil {
		return err
	}
	writePart(w, "text/plain", msg.Text)
	writePart(w, "text/html", msg.HTML)
	return w.Close()
}

// writePart adds one quoted-printable part to the multipart body.
func writePart(w *multipart.Writer, contentType, content string) {
	header := textproto.MIMEHeader{}
	header.Set("Content-Type", contentType+"; charset=utf-8")
	header.Set("Content-Transfer-Encoding", "quoted-printable")
	part, err := w.CreatePart(header)
	if err != nil {
		// The header is fixed and the writer already validated the boundary, so
		// the only failure here is the destination buffer.
		return
	}
	qp := quotedprintable.NewWriter(part)
	_, _ = qp.Write([]byte(content))
	_ = qp.Close()
}

// writeHeader renders the header block. multipart.Writer sorts and writes its
// own part headers, but the message header is written here, in the order the
// fields were set, which is the order a reader expects.
func writeHeader(out *bytes.Buffer, header textproto.MIMEHeader) {
	for _, name := range headerOrder(header) {
		for _, value := range header[name] {
			fmt.Fprintf(out, "%s: %s\r\n", name, value)
		}
	}
}

// headerOrder lists the header names in the order they are written: the fields a
// reader looks at first, then whatever the caller added, sorted so two runs
// produce the same message.
func headerOrder(header textproto.MIMEHeader) []string {
	preferred := []string{
		"From", "To", "Cc", "Subject", "Date", "Message-ID",
		"MIME-Version", "Content-Type", "Content-Transfer-Encoding",
	}
	seen := make(map[string]bool, len(header))
	order := make([]string, 0, len(header))
	for _, name := range preferred {
		if _, ok := header[name]; ok {
			order = append(order, name)
			seen[name] = true
		}
	}
	extra := make([]string, 0, len(header))
	for name := range header {
		if !seen[name] {
			extra = append(extra, name)
		}
	}
	slices.Sort(extra)
	return append(order, extra...)
}

// newBoundary returns a multipart boundary that cannot appear in the bodies.
// A random one is what makes that true: a boundary derived from the content
// could be produced by the content.
func newBoundary() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("mailer: boundary: %w", err)
	}
	return "tango-" + hex.EncodeToString(b[:]), nil
}

// messageID builds the Message-ID a receiving server records. The domain is the
// sender's, so the identifier belongs to the deployment that sent the message.
func messageID(from string) string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("<%d@%s>", time.Now().UnixNano(), localName(from))
	}
	return fmt.Sprintf("<%s@%s>", hex.EncodeToString(b[:]), localName(from))
}
