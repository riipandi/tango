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
// The harness passes --data-dir pointing at a fresh temp directory: the test
// working directory is cmd/, which has no storage/ next to it, and the storage
// check would otherwise fail for a reason the test does not care about.
//
// The root command installs a no-op ExitErrHandler: without it cli.HandleExitCoder
// calls os.Exit, which would end the test binary instead of returning the error
// this test asserts on.
func runHealthCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return runHealthCmdIn(t, t.TempDir(), args...)
}

// runHealthCmdIn runs the health command with an explicit data directory.
func runHealthCmdIn(t *testing.T, dataDir string, args ...string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	root := &cli.Command{
		Name:     "tango",
		Writer:   &out,
		Commands: []*cli.Command{healthCheckCmd},
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "env-file", Usage: "Load environment variables from a file"},
			&cli.StringFlag{Name: "data-dir", Usage: "Set the Application data directory"},
		},
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
	}
	args = append([]string{"tango", healthCheckCmd.Name, "--data-dir=" + dataDir}, args...)
	err := root.Run(context.Background(), args)
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
	require.Len(t, details, 2)

	// Ordered by check name, so the CLI and the API always agree.
	first, ok := details[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "postgres", first["name"])
	assert.Equal(t, "up", first["status"])

	second, ok := details[1].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "storage", second["name"])
	assert.Equal(t, "up", second["status"])
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
	assert.Contains(t, out, "checks: 2 up, 0 down")
	assert.Contains(t, out, "postgres: up (")
	assert.Contains(t, out, "storage: up (")
}

// The storage check must reach the CLI report and use the resolved data
// directory, so an unusable directory fails the command.
func TestHealthReportsUnusableDataDir(t *testing.T) {
	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	// A directory that does not exist: the application would only discover
	// this when it tried to store the first file.
	out, err := runHealthCmdIn(t, filepath.Join(t.TempDir(), "nope"), "--env-file="+envFile)
	require.Error(t, err)

	var exitErr cli.ExitCoder
	require.ErrorAs(t, err, &exitErr)
	assert.Equal(t, exitUnhealthy, exitErr.ExitCode())
	assert.Contains(t, out, "storage: down")
	assert.Contains(t, out, "does not exist")
}

// A world-writable data directory must fail the probe, because it holds uploads
// and certificates.
func TestHealthReportsWorldWritableDataDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}

	container := testutils.StartPostgres(t.Context(), t)
	envFile := healthEnvFile(t, container.NewDatabase(t))

	dir := t.TempDir()
	require.NoError(t, os.Chmod(dir, 0o777))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	out, err := runHealthCmdIn(t, dir, "--env-file="+envFile)
	require.Error(t, err)
	assert.Contains(t, out, "world-writable")
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

// The report names the database that was reached, and never its password: an
// operator pastes this output into a ticket.
func TestPostgresTargetOmitsCredentials(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "url form",
			dsn:  "postgresql://postgres:supersecret@db.internal:5432/tango?sslmode=disable",
			want: "db.internal:5432/tango",
		},
		{
			name: "key value form",
			dsn:  "host=db.internal port=5432 dbname=tango user=postgres password=supersecret",
			want: "db.internal:5432/tango",
		},
		{
			name: "unparsable dsn reports nothing",
			dsn:  "://not-a-dsn",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := postgresTarget(tt.dsn)

			assert.Equal(t, tt.want, target)
			assert.NotContains(t, target, "supersecret")
		})
	}
}
