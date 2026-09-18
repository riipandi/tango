// Package testutils provides shared integration-test helpers.
package testutils

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	tcmp "github.com/testcontainers/testcontainers-go/modules/mailpit"
)

// Mailpit is a running Mailpit container.
type Mailpit struct {
	// SMTPAddr is the host:port of the SMTP listener.
	SMTPAddr string

	// APIURL is the HTTP API base URL (http://host:port).
	APIURL string

	// Username and Password are the relay credentials.
	Username string
	Password string
}

// mailpitImage is the Mailpit image used by integration tests.
const mailpitImage = "docker.io/axllent/mailpit:latest"

var (
	mailpitOnce   sync.Once
	sharedMailpit *Mailpit
	sharedErr     error
)

// StartMailpit returns the shared Mailpit container for the test binary.
func StartMailpit(ctx context.Context, t testing.TB) *Mailpit {
	t.Helper()

	SkipWithoutDocker(t)

	mailpitOnce.Do(func() {
		container, startErr := tcmp.Run(ctx, mailpitImage,
			tcmp.WithSMTPAuth("maileruser1", "mailerpass1"),
		)
		if startErr != nil {
			sharedErr = startErr
			return
		}

		smtpAddr, endpointErr := container.SMTPEndpoint(ctx)
		if endpointErr != nil {
			sharedErr = endpointErr
			return
		}
		apiURL, endpointErr := container.HTTPURL(ctx)
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
