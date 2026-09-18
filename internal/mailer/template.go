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

// templatePair holds parsed HTML and text templates.
type templatePair struct {
	html *htmltemplate.Template // auto-escaping
	text *texttemplate.Template
}

// templateStore renders templates and caches parsed pairs.
type templateStore struct {
	fs    fs.FS
	logo  string
	mu    sync.RWMutex
	cache map[string]*templatePair
}

func newTemplateStore(source fs.FS, logo string) *templateStore {
	return &templateStore{fs: source, logo: logo, cache: make(map[string]*templatePair)}
}

// render produces HTML and text bodies.
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

// pair returns a cached pair or parses it on first use.
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

// Warm parses every compiled template at startup so a broken or
// half-compiled template pair fails the boot, not the first send.
func (s *templateStore) Warm() error {
	matches, err := fs.Glob(s.fs, "*_html.tmpl")
	if err != nil {
		return fmt.Errorf("list templates: %w", err)
	}
	for _, match := range matches {
		name := strings.TrimSuffix(match, "_html.tmpl")
		if _, err := s.pair(name); err != nil {
			return err
		}
	}
	return nil
}
