package launcher

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
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
	port := freePort(t)

	runErr := make(chan error, 1)
	go func() {
		serve := &ServeCmd{Port: fmt.Sprintf("%d", port)}
		runErr <- serve.Run(&CLI{})
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/users", port)
	var resp *http.Response
	require.Eventually(t, func() bool {
		r, err := http.Get(url) //nolint:bodyclose
		if err != nil {
			return false
		}
		resp = r
		return true
	}, 5*time.Second, 50*time.Millisecond, "server must come up")
	resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// The signal path: ServeCmd.Run registered SIGTERM handling.
	require.NoError(t, syscall.Kill(os.Getpid(), syscall.SIGTERM))
	select {
	case err := <-runErr:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("serve.Run did not return after SIGTERM")
	}
}
