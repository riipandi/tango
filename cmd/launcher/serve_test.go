package launcher

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/require"
)

// freePort reserves a port and immediately releases it.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// TestServeRunLifecycle boots the real server stack (config, logger,
// registry, transport) and tears it down with SIGTERM — the same
// path a production SIGTERM takes.
func TestServeRunLifecycle(t *testing.T) {
	t.Setenv("APP_LOG_LEVEL", "error") // keep test output quiet
	// The server pings the database on boot (fail fast), so the
	// lifecycle test points it at the shared testcontainer.
	pg := testutils.StartPostgres(t.Context(), t)
	t.Setenv("DATABASE_URL", pg.DSN)
	port := freePort(t)

	runErr := make(chan error, 1)
	go func() {
		serve := &ServeCmd{Port: fmt.Sprintf("%d", port)}
		runErr <- serve.Run(&CLI{})
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/users", port)

	// Wait for the server to accept connections; every probe body
	// is closed inside the closure.
	require.Eventually(t, func() bool {
		resp, err := http.Get(url)
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 50*time.Millisecond, "server must come up")

	// One full request-response, closed deterministically.
	resp, err := http.Get(url)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// The signal path: ServeCmd.Run registered SIGTERM handling.
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("serve.Run did not return after SIGTERM")
	}
}
