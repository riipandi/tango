// Package testutils holds shared integration-test helpers: short-
// lived service containers spun up per test via testcontainers, so
// tests never depend on a running compose stack.
package testutils

import (
	"context"
	"sync"
	"testing"
	"time"

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

var (
	mailpitOnce   sync.Once
	sharedMailpit *Mailpit
	sharedErr     error
)

// StartMailpit returns the process-wide shared Mailpit container:
// the first call starts it, later calls in the same test binary
// reuse it, and the testcontainers reaper terminates it when the
// binary exits. Tests isolate their data with unique recipients.
func StartMailpit(ctx context.Context, t testing.TB) *Mailpit {
	t.Helper()

	mailpitOnce.Do(func() {
		container, startErr := tcre.Run(ctx, mailpitImage,
			tcre.WithEnv(map[string]string{
				"MP_SMTP_AUTH":                "maileruser1:mailerpass1",
				"MP_SMTP_AUTH_ALLOW_INSECURE": "true",
			}),
			tcre.WithAdditionalWaitStrategy(
				wait.ForListeningPort("1025/tcp").
					WithStartupTimeout(30*time.Second),
			),
		)
		if startErr != nil {
			sharedErr = startErr
			return
		}

		smtpAddr, endpointErr := container.PortEndpoint(ctx, "1025/tcp", "")
		if endpointErr != nil {
			sharedErr = endpointErr
			return
		}
		apiURL, endpointErr := container.PortEndpoint(ctx, "8025/tcp", "http")
		if endpointErr != nil {
			sharedErr = endpointErr
			return
		}

		sharedMailpit = &Mailpit{
			SMTPAddr: smtpAddr,
			APIURL:   apiURL,
			Username: "maileruser1",
			Password: "mailerpass1",
		}
	})

	require.NoError(t, sharedErr, "start mailpit container (docker daemon required)")
	return sharedMailpit
}
