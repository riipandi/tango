# Mailer

Mailer is tango's outbound email path. One `*Service` pairs an SMTP client with the React Email
templates compiled into the binary; the composition root builds it from `config.Mailer` and the
process logger.

> **Relation to the registry:** `internal/registry` registers the service, `serve` resolves it
> before the listener opens, and a feature receives it. It does not construct one. The mailer is
> optional — an empty `mailer.smtp_host` builds a mailer that refuses to send, and that is not a
> deployment failure.

## Features

- **One call to send** — `Send` renders the named template and submits it; a caller supplies the
  recipients, the subject, and the template's fields
- **Two bodies, always** — every message carries the HTML and the plain-text rendering, so a client
  that refuses HTML still shows the message
- **Blind recipients stay blind** — `Bcc` is an envelope recipient only and is never written into
  the headers
- **Encrypted where possible** — implicit TLS (`mailer.smtp_secure`) or STARTTLS when the server
  advertises it; a server offering neither is sent to in the clear and reported as a warning
- **One attempt budget** — `mailer.timeout` bounds the dial, the handshake, every command, and the
  message body. The caller's context ends the session
- **Errors a caller can match** — `ErrNotConfigured`, `ErrNetwork`, `ErrTimeout`, `ErrCanceled`,
  `ErrAuth`, `ErrRejected`, `ErrTemporary` (`errors.Is`)
- **No credentials in a log line** — the target is `host:port`; the username and the password are
  never rendered
- **Templates checked at construction** — the compiled files are parsed as HTML/text pairs when the
  service is built, so a broken build fails the run rather than the first send
- **Escaping where it matters** — the HTML body is rendered by `html/template` (a name carrying
  markup stays text), the plain-text body by `text/template` (it is not HTML)

## Wiring

```go
service := do.MustInvoke[*mailer.Service](injector)
err := service.Send(ctx, mailer.Request{
    To:       []string{"andi@example.com"},
    Subject:  "Reset your password",
    Template: mailer.TemplatePasswordReset,
    View: mailer.View{Data: mailer.PasswordResetData{
        Email:     "andi@example.com",
        ResetLink: "https://app.example.com/reset?token=…",
    }},
})
```

A feature that only wants the SMTP half takes `service.Mailer()`; one that only wants a rendered
body takes `service.Templates()`.

## Templates

The sources live in `email/templates/*.tsx` and are compiled by the Vite build
(`plugins/plugin-email.ts`) into `web/email/<name>_{html,text}.tmpl`, which `web/embed.go` embeds.
A template's fields are the `{{.Data.…}}` names in its `TemplateProps`, and the Go-side struct that
supplies them lives in `internal/mailer/data.go`. The field names must match exactly: a Go template
resolves a field by its own name, so a renamed field fails the render rather than printing nothing.

Adding a template means a `.tsx` file, a struct in `data.go`, and a fixture in
`internal/mailer/templates_test.go` — the test renders every template the binary carries, so a
missing fixture or a renamed field fails there.

## Defaults

| Key | Default |
| --- | --- |
| `mailer.from_email` | `mailer@example.com` |
| `mailer.from_name` | `Tango Mailer` |
| `mailer.smtp_host` | empty — the mailer is off |
| `mailer.smtp_port` | `587` |
| `mailer.smtp_secure` | `false` (STARTTLS when offered) |
| `mailer.timeout` | 15s |

Durations in the config file are seconds.

## Proving it works

```sh
docker compose -f docker/compose.yaml up -d mailpit
task mailer:smoke -- --to=you@example.com
```

Open <http://localhost:8025> to read the message back. `internal/mailer/integration_test.go` runs
the same path against `testutils.StartMailpit`, so it needs a Docker daemon and skips without one.
