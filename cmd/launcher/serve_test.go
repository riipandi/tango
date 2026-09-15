package launcher

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/riipandi/tango/database"
	"github.com/riipandi/tango/pkg/testutils"
	"github.com/stretchr/testify/require"
)

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
	// The server expects a migrated schema (deploy order: migrate → serve);
	// the jwks bootstrap queries on start.
	if _, err := database.MigrateUp(t.Context(), pg.DSN); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	port := freePort(t)

	runErr := make(chan error, 1)
	go func() {
		serve := &ServeCmd{Port: fmt.Sprintf("%d", port)}
		runErr <- serve.Run(&CLI{})
	}()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/healthz", port)

	// Wait for the server to accept connections; every probe body
	// is closed inside the closure. Budget covers first-boot RSA
	// key generation alongside the DB ping.
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		} else {
			t.Logf("boot probe: %v", err)
		}

		select {
		case runErr := <-runErr:
			t.Fatalf("serve.Run exited before serving: %v", runErr)
		case <-time.After(100 * time.Millisecond):
		}
	}

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
