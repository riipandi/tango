package mailer

import (
	"fmt"
	htmltemplate "html/template"
	"io/fs"
	"strings"
	"sync"
	texttemplate "text/template"

	"github.com/riipandi/tango/internal/config"
)

// templatePair holds one template's parsed renderers.
type templatePair struct {
	html *htmltemplate.Template // context-aware auto-escaping
	text *texttemplate.Template
}

// templateStore renders templates from the embedded FS with an
// in-memory cache: each template parses exactly once per process
// (the FS is immutable, so entries never invalidate).
type templateStore struct {
	fs    fs.FS
	logo  string
	mu    sync.RWMutex
	cache map[string]*templatePair
}

func newTemplateStore(source fs.FS, logo string) *templateStore {
	return &templateStore{fs: source, logo: logo, cache: make(map[string]*templatePair)}
}

// render produces the HTML and plain-text bodies for a template.
func (s *templateStore) render(name, to string, data map[string]any) (string, string, error) {
	pair, err := s.pair(name)
	if err != nil {
		return "", "", err
	}

	view := struct {
		AppName string
		LogoURL string
		Email   string
		Data    map[string]any
	}{AppName: config.AppName, LogoURL: s.logo, Email: to, Data: data}

	var htmlOut, textOut strings.Builder
	if err := pair.html.ExecuteTemplate(&htmlOut, "root", view); err != nil {
		return "", "", fmt.Errorf("render %s html: %w", name, err)
	}
	if err := pair.text.ExecuteTemplate(&textOut, "root", view); err != nil {
		return "", "", fmt.Errorf("render %s text: %w", name, err)
	}
	return htmlOut.String(), textOut.String(), nil
}

// pair returns the cached parse result, parsing on first use with
// a double-checked read-mostly lock.
func (s *templateStore) pair(name string) (*templatePair, error) {
	s.mu.RLock()
	pair, ok := s.cache[name]
	s.mu.RUnlock()
	if ok {
		return pair, nil
	}

	htmlTmpl, err := htmltemplate.ParseFS(s.fs, name+"_html.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}
	textTmpl, err := texttemplate.ParseFS(s.fs, name+"_text.tmpl")
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", name, err)
	}

	pair = &templatePair{html: htmlTmpl, text: textTmpl}
	s.mu.Lock()
	s.cache[name] = pair
	s.mu.Unlock()
	return pair, nil
}
