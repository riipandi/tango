package mailer

import (
	"bytes"
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"path"
	"slices"
	"strings"
	texttemplate "text/template"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/web"
)

// Body is the two renderings of one message. Both are produced for every
// template, so a recipient whose client refuses HTML still reads the message.
type Body struct {
	HTML string
	Text string
}

// View is what a template renders against besides the sender identity: the
// recipient's address where a template shows it, and the template's own fields
// under Data.
//
// Data is a value the template reflects into — a struct with the field names the
// template uses (`{{.Data.ResetLink}}`), or a map with the same keys. A template
// naming a field the value does not have fails the render rather than printing
// an empty string, so a renamed field is caught by the test that renders it.
type View struct {
	// Email is the recipient a template shows, for the templates that name it.
	Email string
	// Data carries the template's own fields.
	Data any
}

// Sender is the identity every template renders with. It is resolved from the
// configuration once, so a template never reads the configuration itself.
type Sender struct {
	AppName string
	LogoURL string
}

// SenderFrom resolves the identity from the configuration.
//
// The logo is an absolute URL, because a mail client fetches it from outside
// this process. The public origin is `app.base_url`: the application serves the
// bundle the image ships in, so its own origin is always right. `app.assets_url`
// is the fallback for a deployment that publishes its assets on another origin
// and names no public base URL.
func SenderFrom(cfg config.Config) Sender {
	origin := cfg.App.BaseURL
	if origin == "" {
		origin = cfg.App.AssetsURL
	}
	return Sender{
		AppName: config.AppName,
		LogoURL: strings.TrimSuffix(origin, "/") + "/images/logoEmail.svg",
	}
}

// Templates renders the React Email templates embedded in the binary.
//
// The HTML and the text rendering of a template are parsed as one unit, so a
// template that cannot be parsed fails construction rather than a send. The
// compiled files are produced by the Vite build (plugins/plugin-email.ts), so a
// parse failure here is a broken build, not a bad configuration.
type Templates struct {
	sender Sender
	// html and text hold one parsed template per name. The two sets are kept
	// apart because html/template escapes what it writes and text/template does
	// not, and a plain-text body must not arrive HTML-escaped.
	html map[string]*htmltemplate.Template
	text map[string]*texttemplate.Template
}

// NewTemplates parses the templates embedded in this binary.
func NewTemplates(sender Sender) (*Templates, error) {
	return NewTemplatesFS(web.EmailTemplates, sender)
}

// NewTemplatesFS parses the templates under dir of fsys. The directory layout is
// the one the Vite build writes: one `<name>_html.tmpl` and one `<name>_text.tmpl`
// per template, each defining the block "root".
//
// It exists so a test renders fixtures without rebuilding the embedded set.
func NewTemplatesFS(fsys fs.FS, sender Sender) (*Templates, error) {
	entries, err := fs.Glob(fsys, "email/*.tmpl")
	if err != nil {
		return nil, fmt.Errorf("mailer: list templates: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("mailer: no email templates are embedded; run the Vite build")
	}

	t := &Templates{
		sender: sender,
		html:   make(map[string]*htmltemplate.Template, len(entries)),
		text:   make(map[string]*texttemplate.Template, len(entries)),
	}
	for _, entry := range entries {
		source, readErr := fs.ReadFile(fsys, entry)
		if readErr != nil {
			return nil, fmt.Errorf("mailer: read %s: %w", entry, readErr)
		}
		name, kind := templateName(path.Base(entry))
		if name == "" {
			return nil, fmt.Errorf("mailer: %s is not a <name>_<html|text>.tmpl file", entry)
		}
		if err := t.parse(name, kind, source); err != nil {
			return nil, fmt.Errorf("mailer: %s: %w", entry, err)
		}
	}

	// A template with one rendering and not the other cannot produce a body, so
	// the pair is required rather than filled in with an empty half.
	for name := range t.html {
		if _, ok := t.text[name]; !ok {
			return nil, fmt.Errorf("mailer: template %q has no text rendering", name)
		}
	}
	for name := range t.text {
		if _, ok := t.html[name]; !ok {
			return nil, fmt.Errorf("mailer: template %q has no html rendering", name)
		}
	}
	return t, nil
}

// parse adds one rendering to the set.
func (t *Templates) parse(name, kind string, source []byte) error {
	if kind == kindText {
		parsed, err := texttemplate.New(name).Parse(string(source))
		if err != nil {
			return err
		}
		t.text[name] = parsed
		return nil
	}
	parsed, err := htmltemplate.New(name).Parse(string(source))
	if err != nil {
		return err
	}
	t.html[name] = parsed
	return nil
}

// The two renderings a template name resolves to.
const (
	kindHTML = "html"
	kindText = "text"
)

// templateName splits a compiled file name into the template it belongs to and
// which rendering it is.
func templateName(file string) (name, kind string) {
	switch {
	case strings.HasSuffix(file, "_"+kindHTML+".tmpl"):
		return strings.TrimSuffix(file, "_"+kindHTML+".tmpl"), kindHTML
	case strings.HasSuffix(file, "_"+kindText+".tmpl"):
		return strings.TrimSuffix(file, "_"+kindText+".tmpl"), kindText
	default:
		return "", ""
	}
}

// Names lists the templates this binary carries, sorted. It is what a command
// reports and what a test iterates.
func (t *Templates) Names() []string {
	names := make([]string, 0, len(t.html))
	for name := range t.html {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// Has reports whether a template name is one this binary carries.
func (t *Templates) Has(name string) bool {
	_, ok := t.html[name]
	return ok
}

// Render produces both renderings of one template.
//
// The sender identity comes from the Templates value, so a caller supplies only
// what is specific to this message. An unknown name is refused rather than
// rendered as an empty body.
func (t *Templates) Render(name string, view View) (Body, error) {
	htmlTemplate, ok := t.html[name]
	if !ok {
		return Body{}, fmt.Errorf("mailer: unknown template %q; known: %s",
			name, strings.Join(t.Names(), ", "))
	}
	context := struct {
		AppName string
		LogoURL string
		Email   string
		Data    any
	}{
		AppName: t.sender.AppName,
		LogoURL: t.sender.LogoURL,
		Email:   view.Email,
		Data:    view.Data,
	}

	var body Body
	var htmlOut, textOut bytes.Buffer
	if err := htmlTemplate.ExecuteTemplate(&htmlOut, "root", context); err != nil {
		return Body{}, fmt.Errorf("mailer: render %s html: %w", name, err)
	}
	if err := t.text[name].ExecuteTemplate(&textOut, "root", context); err != nil {
		return Body{}, fmt.Errorf("mailer: render %s text: %w", name, err)
	}
	body.HTML = htmlOut.String()
	body.Text = textOut.String()
	return body, nil
}
