// Package testutils holds shared integration-test helpers: short-
// lived service containers spun up per test via testcontainers, so
// tests never depend on a running compose stack.
package testutils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	tcre "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Mailpit is a running Mailpit container: an SMTP catch-all relay
// with an HTTP API for verifying deliveries.
type Mailpit struct {
	// SMTPAddr is the host:port of the SMTP listener.
	SMTPAddr string

	// APIURL is the HTTP API base URL (http://host:port).
	APIURL string

	// Username and Password are the relay credentials (auth is
	// enforced with MP_SMTP_AUTH, plaintext allowed).
	Username string
	Password string
}

// mailpitImage is intentionally unpinned to match compose.yaml's
// MAILPIT_VERSION default; pin both when pinning one.
const mailpitImage = "axllent/mailpit:latest"

// StartMailpit starts a throwaway Mailpit container that terminates
// itself with the test's cleanup.
func StartMailpit(ctx context.Context, t testing.TB) *Mailpit {
	t.Helper()

	container, err := tcre.Run(ctx, mailpitImage,
		tcre.WithEnv(map[string]string{
			"MP_SMTP_AUTH":                "maileruser1:mailerpass1",
			"MP_SMTP_AUTH_ALLOW_INSECURE": "true",
		}),
		tcre.WithAdditionalWaitStrategy(wait.ForListeningPort("1025/tcp")),
	)
	require.NoError(t, err, "start mailpit container (docker daemon required)")
	t.Cleanup(func() { _ = container.Terminate(context.WithoutCancel(ctx)) })

	smtpAddr, err := container.PortEndpoint(ctx, "1025/tcp", "")
	require.NoError(t, err)
	apiURL, err := container.PortEndpoint(ctx, "8025/tcp", "http")
	require.NoError(t, err)

	return &Mailpit{
		SMTPAddr: smtpAddr,
		APIURL:   apiURL,
		Username: "maileruser1",
		Password: "mailerpass1",
	}
}
