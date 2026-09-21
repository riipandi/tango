package logger_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"encoding/json/jsontext"
	"encoding/json/v2"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
)

// newLogger builds a logger from the default configuration with the console
// rendering captured in a buffer, so a test reads what a person would see.
func newLogger(t *testing.T, adjust func(*config.Config)) (*logger.Logger, *bytes.Buffer) {
	t.Helper()

	cfg := config.Default()
	if adjust != nil {
		adjust(&cfg)
	}

	buf := &bytes.Buffer{}
	log, err := logger.New(cfg, logger.WithWriter(buf))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Shutdown(context.Background())) })

	return log, buf
}

func TestConsoleRendersEveryLevelTheConfigurationAllows(t *testing.T) {
	log, buf := newLogger(t, func(cfg *config.Config) { cfg.Log.Level = config.LogDebug })

	log.Slog().Debug("debug line")
	log.Slog().Info("info line")
	log.Slog().Warn("warn line")
	log.Slog().Error("error line")

	out := buf.String()
	for _, message := range []string{"debug line", "info line", "warn line", "error line"} {
		assert.Contains(t, out, message)
	}
}

func TestConfiguredLevelIsTheLoggerThreshold(t *testing.T) {
	// The threshold lives on the logger, not on a sink, so every destination
	// agrees on what is worth emitting: a file that receives what the console
	// dropped would be a second answer to the same question.
	log, buf := newLogger(t, func(cfg *config.Config) { cfg.Log.Level = config.LogWarn })

	log.Slog().Debug("dropped debug")
	log.Slog().Info("dropped info")
	log.Slog().Warn("kept warn")

	out := buf.String()
	assert.NotContains(t, out, "dropped debug")
	assert.NotContains(t, out, "dropped info")
	assert.Contains(t, out, "kept warn")
}

func TestStructuredFormatWritesOneJSONObjectPerLine(t *testing.T) {
	log, buf := newLogger(t, func(cfg *config.Config) { cfg.Log.Format = config.LogStructured })

	log.Slog().Info("served", "user", "alice", "status", 200)

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 1)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &entry))
	assert.Equal(t, "served", entry["msg"])
	assert.Equal(t, "info", entry["level"])
	assert.Equal(t, "alice", entry["user"], "an attribute must survive as a field")
	assert.Equal(t, float64(200), entry["status"])
}

func TestConsoleStaysPlainTextOffATerminal(t *testing.T) {
	// A redirected run must not carry escape codes: the output is a log, and a
	// reader that greps it would see the codes as text.
	log, buf := newLogger(t, nil)

	log.Slog().Info("plain")

	assert.NotContains(t, buf.String(), "\x1b[")
}

func TestFileSinkWritesTheEntryToTheConfiguredPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "tango.log")

	log, _ := newLogger(t, func(cfg *config.Config) {
		cfg.Log.File.Filename = path
	})

	log.Slog().Info("written to disk", "n", 42)
	require.NoError(t, log.Shutdown(context.Background()))

	raw, err := os.ReadFile(path)
	require.NoError(t, err, "the sink creates the directory tree it was given")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(raw, &entry))
	assert.Equal(t, "written to disk", entry["msg"])
	assert.Equal(t, float64(42), entry["n"])
}

func TestFileSinkKeepsItsOwnJSONForm(t *testing.T) {
	// The console format is what a person reads; a file is read by a machine, so
	// asking for a pretty console must not put escape codes and column padding
	// in the file.
	path := filepath.Join(t.TempDir(), "tango.log")

	log, buf := newLogger(t, func(cfg *config.Config) {
		cfg.Log.Format = config.LogPretty
		cfg.Log.File.Filename = path
	})

	log.Slog().Info("both sinks")
	require.NoError(t, log.Shutdown(context.Background()))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.True(t, jsontext.Value(raw).IsValid(), "the file must hold JSON: %s", raw)
	assert.NotContains(t, string(raw), "\x1b[")
	assert.Contains(t, buf.String(), "both sinks")
}

func TestShutdownIsIdempotent(t *testing.T) {
	// A shutdown path runs once on the happy path and again from a deferred
	// cleanup, so a second call must not fail or write to a closed file.
	path := filepath.Join(t.TempDir(), "tango.log")
	log, _ := newLogger(t, func(cfg *config.Config) { cfg.Log.File.Filename = path })

	require.NoError(t, log.Shutdown(context.Background()))
	require.NoError(t, log.Shutdown(context.Background()))
}

func TestSetDefaultInstallsTheProcessLogger(t *testing.T) {
	// A dependency that logs through slog without being handed a logger must
	// land in the same pipeline, which is what makes one stack enough.
	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })

	log, buf := newLogger(t, nil)
	log.SetDefault()

	slog.Info("from the package default")

	assert.Contains(t, buf.String(), "from the package default")
}

func TestUnsetOTLPVariableDoesNotReachTheSink(t *testing.T) {
	// The configuration is the only source of the endpoint. The exporter reads
	// OTEL_EXPORTER_OTLP_* on its own, so a stray variable in the environment
	// must not decide where a deployment ships its logs.
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://127.0.0.1:1/v1/logs")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer leaked")
	t.Setenv("OTEL_RESOURCE_ATTRIBUTES", "service.name=from-the-shell")

	server := newOTLPTestServer(t)
	cfg := config.Default()
	cfg.Log.OTLP.Enable = true
	cfg.Log.OTLP.Endpoint = server.URL

	log, err := logger.New(cfg, logger.WithWriter(&bytes.Buffer{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Shutdown(context.Background())) })

	log.Slog().Info("shipped")
	require.NoError(t, log.Shutdown(context.Background()))

	received := server.requests()
	require.Len(t, received, 1)
	assert.Equal(t, "/v1/logs", received[0].path, "the protocol path comes from the config endpoint")
	assert.Empty(t, received[0].authorization, "a header from the environment must not be sent")
	assert.NotContains(t, string(received[0].body), "from-the-shell",
		"the resource is built here, not read from OTEL_RESOURCE_ATTRIBUTES")
	assert.Contains(t, string(received[0].body), config.AppIdentifier,
		"the service is the one this program names")
}

func TestOTLPSinkShipsToTheConfiguredEndpoint(t *testing.T) {
	server := newOTLPTestServer(t)

	cfg := config.Default()
	cfg.Log.OTLP.Enable = true
	cfg.Log.OTLP.Endpoint = server.URL

	log, err := logger.New(cfg, logger.WithWriter(&bytes.Buffer{}))
	require.NoError(t, err)

	log.Slog().Info("collected")
	require.NoError(t, log.Shutdown(context.Background()))

	received := server.requests()
	require.Len(t, received, 1)
	assert.NotEmpty(t, received[0].body, "the record must carry a protobuf payload")
}

func TestOTLPEndpointPathIsUsedWhenTheConfigurationNamesOne(t *testing.T) {
	server := newOTLPTestServer(t)

	cfg := config.Default()
	cfg.Log.OTLP.Enable = true
	cfg.Log.OTLP.Endpoint = server.URL + "/collector/v1/logs"

	log, err := logger.New(cfg, logger.WithWriter(&bytes.Buffer{}))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, log.Shutdown(context.Background())) })

	log.Slog().Info("routed")
	require.NoError(t, log.Shutdown(context.Background()))

	received := server.requests()
	require.Len(t, received, 1)
	assert.Equal(t, "/collector/v1/logs", received[0].path)
}

func TestDisabledOTLPSinkDialsNothing(t *testing.T) {
	// The default configuration ships nowhere: a local checkout must run with no
	// collector, and a disabled backend is never dialled.
	server := newOTLPTestServer(t)

	log, _ := newLogger(t, func(cfg *config.Config) {
		cfg.Log.OTLP.Enable = false
		cfg.Log.OTLP.Endpoint = server.URL
	})
	log.Slog().Info("local only")
	require.NoError(t, log.Shutdown(context.Background()))

	assert.Empty(t, server.requests())
}
