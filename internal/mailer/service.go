package mailer

import (
	"context"
	"errors"
	"fmt"
)

// Request is one templated email: who receives it, what it says, and which
// template renders it. To, Cc, and Bcc are bare addresses.
type Request struct {
	To  []string
	Cc  []string
	Bcc []string
	// Subject is the rendered subject line, not a template name.
	Subject string
	// Template names one of the templates this binary carries. It is refused
	// when unknown, so a typo is an error rather than an empty message.
	Template string
	// View carries what the template shows.
	View View
	// Headers adds header lines such as Reply-To. They are checked the same way
	// a Message's are.
	Headers map[string]string
}

// Service is the mailer and the templates as one value: what a feature receives
// and what the composition root builds. The two are separate types because
// rendering and sending fail for different reasons, and a caller that only wants
// one of them can still hold it.
type Service struct {
	mailer    *Mailer
	templates *Templates
}

// NewService pairs a mailer with a template set.
func NewService(m *Mailer, t *Templates) *Service {
	return &Service{mailer: m, templates: t}
}

// Mailer is the SMTP half, for a message this package did not render.
func (s *Service) Mailer() *Mailer { return s.mailer }

// Templates is the rendering half, for a body sent some other way.
func (s *Service) Templates() *Templates { return s.templates }

// Configured reports whether a mail server is named. A feature that sends mail
// as a side effect checks this to skip the work rather than to fail.
func (s *Service) Configured() bool { return s.mailer.Configured() }

// Send renders one template and submits the result.
//
// The template is rendered before the session is opened, so a broken template
// costs no connection and no credential exchange.
func (s *Service) Send(ctx context.Context, req Request) error {
	if s.mailer == nil || s.templates == nil {
		return errors.New("mailer: service is not built")
	}
	if req.Template == "" {
		return errors.New("mailer: request names no template")
	}
	body, err := s.templates.Render(req.Template, req.View)
	if err != nil {
		return err
	}
	return s.mailer.Send(ctx, Message{
		To:      req.To,
		Cc:      req.Cc,
		Bcc:     req.Bcc,
		Subject: req.Subject,
		HTML:    body.HTML,
		Text:    body.Text,
		Headers: req.Headers,
	})
}

// String names what the service can send without revealing a credential.
func (s *Service) String() string {
	if s == nil || s.mailer == nil {
		return "mailer: not built"
	}
	if s.templates == nil {
		return s.mailer.String()
	}
	return fmt.Sprintf("%s, %d templates", s.mailer.String(), len(s.templates.Names()))
}
