package mailer

import (
	"crypto/tls"
	"net"
	"testing"

	"github.com/emersion/go-smtp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/riipandi/tango/pkg/testutils"
)

func TestStartTLSUnsupportedMatchesTheLibraryMessage(t *testing.T) {
	// The library exports no sentinel for "this server has no STARTTLS", so
	// startTLSUnsupported is the message itself. A Mailpit container without TLS
	// answers exactly that, which pins the text: a library change fails this
	// test rather than quietly sending a session in the clear.
	server := testutils.StartMailpit(t.Context(), t)

	conn, err := net.Dial("tcp", server.SMTPAddr)
	require.NoError(t, err)
	defer conn.Close()

	_, err = smtp.NewClientStartTLS(conn, &tls.Config{ServerName: "localhost"})
	require.Error(t, err)
	assert.Equal(t, startTLSUnsupported, err.Error())
	assert.True(t, isStartTLSUnsupported(err))
}

func TestIsStartTLSUnsupportedRejectsOtherFailures(t *testing.T) {
	assert.False(t, isStartTLSUnsupported(nil))
	assert.False(t, isStartTLSUnsupported(errNoAuthMechanism))
}
