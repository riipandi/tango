package main

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/pkg/envfile"
	"github.com/riipandi/tango/pkg/testutils"
)

// passing is a check function that always succeeds.
func passing(context.Context) error { return nil }

// runHealthCmd executes the health command with args and returns stdout.
//
// The root command installs a no-op ExitErrHandler: without it cli.HandleExitCoder
// calls os.Exit, which would end the test binary instead of returning the error
// this test asserts on.
func runHealthCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := &cli.Command{
		Name:     "tango",
		Writer:   &out,
		Commands: []*cli.Command{healthCheckCmd},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "env-file", Usage: "Load environment variables from a file"},
		},
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	err := root.Run(context.Background(), append([]string{"tango", healthCheckCmd.Name}, args...))
	return out.String(), err
}

// healthEnvFile writes an env file pointing at dsn.
func healthEnvFile(t *testing.T, dsn string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), ".env.local")
	require.NoError(t, os.WriteFile(path, []byte(envfile.DatabaseURL+"="+dsn+"\n"), 0o600))
	return path
}

func TestHealthReportsUpAgainstAReachableDatabase(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile)
	require.NoError(t, err)
	assert.Contains(t, out, "status: healthy")
}

func TestHealthReportsDownAgainstAnUnreachableDatabase(t *testing.T) {
	// A port nothing listens on: the probe must fail, not hang.
	envFile := healthEnvFile(t, "postgresql://postgres:securedb@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")

	out, err := runHealthCmd(t, "--env-file="+envFile, "--timeout=3s")
	require.Error(t, err)

	// The exit code must be distinct from a generic failure, so a script can
	// tell "the service is unhealthy" from "the command could not run".
	var exitErr cli.ExitCoder
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, exitUnhealthy, exitErr.ExitCode())
	assert.Contains(t, out, "status: unhealthy")
	assert.Contains(t, out, "postgres")
}

// A missing DSN is a usage error, not an unhealthy service: it must not report
// exit code 3.
func TestHealthFailsWithoutDatabaseURL(t *testing.T) {
	t.Setenv(envfile.DatabaseURL, "")
	t.Setenv("HOME", t.TempDir())

	out, err := runHealthCmd(t)
	require.ErrorIs(t, err, ErrDatabaseURLUnset)
	assert.NotContains(t, out, "unhealthy")
}

// --json must print a machine-readable result with the same shape as the REST
// body's data field.
func TestHealthJSONOutput(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile, "--json")
	require.NoError(t, err)

	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &result))
	assert.Equal(t, "healthy", result["status"])
	assert.Contains(t, result, "duration_ms")

	// Details are an ordered array, so the CLI output and the API body have the
	// same shape and the same order.
	details, ok := result["details"].([]any)
	require.True(t, ok, "details must be an array")
	require.Len(t, details, 1)

	postgres, ok := details[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "postgres", postgres["name"])
	assert.Equal(t, "up", postgres["status"])
}

// --short must print one word, so a shell script reads it without parsing.
func TestHealthShortOutput(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile, "--short")
	require.NoError(t, err)
	assert.Equal(t, "healthy\n", out)

	// An unreachable database reports the other word and the failing exit code.
	badEnvFile := healthEnvFile(t, "postgresql://postgres:securedb@127.0.0.1:1/none?sslmode=disable")
	out, err = runHealthCmd(t, "--env-file="+badEnvFile, "--short", "--timeout=3s")
	require.Error(t, err)
	assert.Equal(t, "unhealthy\n", out)
}

// The default output is the human-readable report, and it is stable enough to
// assert its shape.
func TestHealthTextOutput(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile)
	require.NoError(t, err)

	assert.Contains(t, out, "name: tango")
	assert.Contains(t, out, "version: "+config.AppVersion)
	assert.Contains(t, out, "status: healthy")
	assert.Contains(t, out, "checks: 1 up, 0 down")
	assert.Contains(t, out, "CHECK")
	assert.Contains(t, out, "postgres  up")
}

// Every failing check must be reported, not just the first.
func TestHealthTextReportsEveryFailure(t *testing.T) {
	result := health.Result{
		Status: health.GlobalUnhealthy,
		Details: map[string]health.CheckResult{
			"postgres": {Name: "postgres", Status: health.StatusDown, Error: "refused"},
			"valkey":   {Name: "valkey", Status: health.StatusDown, Error: "refused"},
		},
	}

	var out strings.Builder
	require.NoError(t, health.WriteText(&out, result))
	assert.Contains(t, out.String(), "postgres")
	assert.Contains(t, out.String(), "valkey")
}

// The CLI must not define a second rendering of the result: --json and the REST
// data field are the same object.
func TestHealthJSONMatchesTheAPIShape(t *testing.T) {
	checker := health.NewChecker(
		health.WithCheck(health.Check{Name: "postgres", Check: passing}),
		health.WithInfo(map[string]string{"name": "tango"}),
	)

	encoded, err := json.Marshal(checker.Check(t.Context()))
	require.NoError(t, err)

	var cli map[string]any
	require.NoError(t, json.Unmarshal(encoded, &cli))

	recorder := httptest.NewRecorder()
	health.Handler(checker).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	var envelope map[string]any
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &envelope))
	api, ok := envelope["data"].(map[string]any)
	require.True(t, ok)

	// The whole-call duration is measured per call, so it differs by design.
	// Everything else — the status, the ordered details, the info — must match.
	delete(cli, "duration_ms")
	delete(api, "duration_ms")
	assert.Equal(t, api, cli)
}

// --short must win over --json: a caller that asks for one word gets one word.
func TestHealthShortWinsOverJSON(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile, "--short", "--json")
	require.NoError(t, err)
	assert.Equal(t, "healthy\n", out)
}

// --no-cache must run the check again instead of reusing a cached result.
func TestHealthNoCacheFlagExists(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	out, err := runHealthCmd(t, "--env-file="+envFile, "--no-cache", "--short")
	require.NoError(t, err)
	assert.Equal(t, "healthy\n", out)
}
