package mailer

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/emersion/go-sasl"
	gosmtp "github.com/emersion/go-smtp"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/web"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"welcome_html.tmpl": &fstest.MapFile{Data: []byte(`{{define "root"}}<img src="{{.LogoURL}}"/><p>Hello {{.Data.UserFullName}}, welcome to {{.AppName}} <a href="{{.Data.Link}}">go</a></p>{{end}}`)},
		"welcome_text.tmpl": &fstest.MapFile{Data: []byte(`{{define "root"}}Hello {{.Data.UserFullName}}, welcome to {{.AppName}}. Link: {{.Data.Link}}{{end}}`)},
	}
}

func TestRenderBuildsBothBodies(t *testing.T) {
	store := newTemplateStore(testFS(), "http://localhost:3000/logo.png")

	html, text, err := store.render("welcome", "alice@example.test", map[string]any{
		"UserFullName": "Alice <script>alert(1)</script>",
		"Link":         "https://app.test/verify?token=abc",
	})
	require.NoError(t, err)

	assert.Contains(t, text, "Hello Alice <script>alert(1)</script>, welcome to "+config.AppName)
	assert.Contains(t, text, "https://app.test/verify?token=abc")

	// HTML output escapes user data.
	assert.Contains(t, html, "Hello Alice &lt;script&gt;")
	assert.NotContains(t, html, "<script>alert(1)</script>")
	assert.Contains(t, html, "http://localhost:3000/logo.png")
}

func TestRenderUnknownTemplate(t *testing.T) {
	store := newTemplateStore(testFS(), "")
	_, _, err := store.render("missing", "x@tango.test", nil)
	require.ErrorContains(t, err, "parse template")
}

func TestTemplateCacheParsesOnce(t *testing.T) {
	store := newTemplateStore(testFS(), "")

	first, err := store.pair("welcome")
	require.NoError(t, err)

	for range 5 {
		second, err := store.pair("welcome")
		require.NoError(t, err)
		assert.Same(t, first, second, "cached pair must be reused")
	}
}

// captureBackend is a fake SMTP relay.
type captureBackend struct {
	mu       sync.Mutex
	messages []capturedMessage
}

type capturedMessage struct {
	from    string
	rcpt    string
	content []byte
}

func (b *captureBackend) NewSession(_ *gosmtp.Conn) (gosmtp.Session, error) {
	return &captureSession{backend: b}, nil
}

type captureSession struct {
	backend *captureBackend
	from    string
	to      string
}

func (s *captureSession) AuthMechanisms() []string { return []string{sasl.Plain} }

func (s *captureSession) Auth(mech string) (sasl.Server, error) {
	return sasl.NewPlainServer(func(_, _, _ string) error { return nil }), nil
}

func (s *captureSession) Mail(from string, _ *gosmtp.MailOptions) error {
	s.from = from
	return nil
}

func (s *captureSession) Rcpt(to string, _ *gosmtp.RcptOptions) error {
	s.to = to
	return nil
}

func (s *captureSession) Data(raw io.Reader) error {
	content, err := io.ReadAll(raw)
	s.backend.mu.Lock()
	s.backend.messages = append(s.backend.messages, capturedMessage{
		from: s.from, rcpt: s.to, content: content,
	})
	s.backend.mu.Unlock()
	return err
}

func (s *captureSession) Reset()        {}
func (s *captureSession) Logout() error { return nil }

// newTestMailer starts an in-process SMTP relay and mailer.
func newTestMailer(t *testing.T) (Mailer, *captureBackend, int) {
	t.Helper()

	backend := &captureBackend{}
	server := gosmtp.NewServer(backend)
	server.AllowInsecureAuth = true

	listener := newSMTPListener(t)
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	// Close the listener first so Serve can exit before stopping the server.
	t.Cleanup(func() {
		listener.Close()
		server.Close()
		if srvErr := <-serveErr; srvErr != nil && !errors.Is(srvErr, net.ErrClosed) && !errors.Is(srvErr, gosmtp.ErrServerClosed) {
			t.Errorf("smtp relay: %v", srvErr)
		}
	})

	cfg := config.MailerConfig{
		FromEmail:    "noreply@tango.test",
		FromName:     "Tango Test",
		SMTPHost:     "127.0.0.1",
		SMTPPort:     portOf(t, listener),
		SMTPUsername: "user",
		SMTPPassword: "pass",
	}
	mailer := New(cfg, Options{
		Templates: testFS(),
		Logger:    logger.NewMock(),
	})
	return mailer, backend, cfg.SMTPPort
}

func TestSendDeliversMultipartMessage(t *testing.T) {
	mailer, backend, _ := newTestMailer(t)

	err := mailer.Send(t.Context(), Message{
		To:       "alice@example.test",
		Subject:  "Verify your email",
		Template: "welcome",
		Data: map[string]any{
			"UserFullName": "Alice",
			"Link":         "https://app.test/verify?token=abc",
		},
	})
	require.NoError(t, err)

	require.Len(t, backend.messages, 1)
	received := backend.messages[0]

	assert.Equal(t, "noreply@tango.test", received.from)
	assert.Equal(t, "alice@example.test", received.rcpt)

	content := string(received.content)
	assert.Contains(t, content, `From: "Tango Test" <noreply@tango.test>`)
	assert.Contains(t, content, "To: <alice@example.test>")
	assert.Contains(t, content, "Subject: Verify your email")
	assert.Contains(t, content, "multipart/alternative")
	assert.Contains(t, content, "text/plain")
	assert.Contains(t, content, "text/html")
	assert.Contains(t, content, "Hello Alice, welcome to "+config.AppName)
}

func TestSendRejectsInvalidInput(t *testing.T) {
	mailer, backend, _ := newTestMailer(t)

	assert.ErrorContains(t, mailer.Send(t.Context(), Message{
		Template: "welcome", Data: map[string]any{},
	}), "recipient is empty")

	assert.ErrorContains(t, mailer.Send(t.Context(), Message{
		To: "alice@example.test", Data: map[string]any{},
	}), "template is empty")

	assert.ErrorContains(t, mailer.Send(t.Context(), Message{
		To:       "not-an-email",
		Template: "welcome",
	}), "invalid recipient")

	assert.Empty(t, backend.messages)
}

// TestSettingsSourceOverridesPerSend checks per-send settings and fallback.
func TestSettingsSourceOverridesPerSend(t *testing.T) {
	mailer, backend, relayPort := newTestMailer(t)
	setter, ok := mailer.(SettingsSourceSetter)
	require.True(t, ok, "smtp mailer accepts a settings source")

	msg := Message{
		To: "alice@example.test", Subject: "relay switch", Template: "welcome",
		Data: map[string]any{"UserFullName": "Alice", "Link": "https://app.test/x"},
	}

	calls := 0
	setter.SetSettingsSource(func(ctx context.Context) (config.MailerConfig, error) {
		calls++
		return config.MailerConfig{
			FromEmail: "switched@tango.test", FromName: "Switched",
			SMTPHost: "127.0.0.1", SMTPPort: relayPort,
		}, nil
	})

	require.NoError(t, mailer.Send(t.Context(), msg))
	require.Len(t, backend.messages, 1)
	assert.Contains(t, string(backend.messages[0].content), `From: "Switched" <switched@tango.test>`)
	assert.Equal(t, 1, calls, "source resolves per send")

	// A failing source falls back to the static config.
	setter.SetSettingsSource(func(ctx context.Context) (config.MailerConfig, error) {
		return config.MailerConfig{}, errors.New("appconfig down")
	})
	require.NoError(t, mailer.Send(t.Context(), msg))
	require.Len(t, backend.messages, 2)
	assert.Contains(t, string(backend.messages[1].content), `From: "Tango Test" <noreply@tango.test>`)
}

func TestSendUnknownTemplateFails(t *testing.T) {
	mailer, backend, _ := newTestMailer(t)

	err := mailer.Send(t.Context(), Message{
		To:       "alice@example.test",
		Subject:  "x",
		Template: "missing",
	})
	require.ErrorContains(t, err, "parse template")
	assert.Empty(t, backend.messages)
}

// TestRealEmbeddedTemplatesRender executes every embedded template.
func TestRealEmbeddedTemplatesRender(t *testing.T) {
	templates, err := fs.Sub(web.EmailTemplates, "email")
	require.NoError(t, err)

	store := newTemplateStore(templates, "")

	matches, err := fs.Glob(templates, "*_html.tmpl")
	require.NoError(t, err)
	require.NotEmpty(t, matches, "embedded templates must be present")

	for _, match := range matches {
		name := strings.TrimSuffix(match, "_html.tmpl")
		html, text, rerr := store.render(name, "recipient@tango.test", map[string]any{
			"UserFullName":      "Test User",
			"VerificationLink":  "https://app.test/verify",
			"ResetLink":         "https://app.test/reset",
			"LoginLink":         "https://app.test/login",
			"LoginLinkWithCode": "https://app.test/login",
			"Code":              "123456",
			"ConfirmLink":       "https://app.test/confirm",
			"ExpiresAt":         time.Now(), // templates call .Format
			"ExpirationString":  "24 hours",
			"ApiKeyName":        "test-key",
			"IPAddress":         "127.0.0.1",
			"City":              "Jakarta",
			"Country":           "ID",
			"Device":            "Chrome",
			"DateTime":          time.Now(), // templates call .Format
			"Name":              "Test User",
			"NewEmail":          "new@tango.test",
			"OldEmail":          "old@tango.test",
		})
		require.NoError(t, rerr, "template %s", name)
		assert.NotEmpty(t, html, "template %s", name)
		assert.NotEmpty(t, text, "template %s", name)
	}
}

// newSMTPListener reserves a loopback port.
func newSMTPListener(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return listener
}

func portOf(t *testing.T, listener net.Listener) int {
	t.Helper()
	return listener.Addr().(*net.TCPAddr).Port
}
