package launcher

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSize(t *testing.T) {
	assert.Equal(t, "2.00 MB", formatSize(2*1024*1024))
	assert.Equal(t, "1.50 MB", formatSize(1536*1024))
	assert.Equal(t, "512.0 KB", formatSize(512*1024))
	assert.Equal(t, "0.5 KB", formatSize(512))
}

// captureStdout redirects os.Stdout so fmt.Print* output is captured.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	require.NoError(t, w.Close())
	out := make([]byte, 4096)
	n, _ := r.Read(out)
	return string(out[:n])
}

func TestHealthStaticPrintsBinaryInfo(t *testing.T) {
	out := runCommand(t, "hc")

	assert.Contains(t, out, "status:    healthy")
	exe, err := os.Executable()
	require.NoError(t, err)
	assert.Contains(t, out, exe)
}

func TestHealthLiveOK(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	// runCommand already captures command output.
	out := runCommand(t, "hc", "--live", "--addr", upstream.URL)
	assert.Contains(t, out, "ok")
}

func TestMigrateSubcommands(t *testing.T) {
	assert.Contains(t, runCommand(t, "migrate", "up"), "migrate up: not yet implemented")
	assert.Contains(t, runCommand(t, "migrate", "down"), "migrate down: not yet implemented")
	assert.Contains(t, runCommand(t, "migrate", "status"), "migrate status: not yet implemented")
}
