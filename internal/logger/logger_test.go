package logger

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.loglayer.dev/v3"
)

func TestNewStructuredConsole(t *testing.T) {
	log, closer, err := New(Options{Level: "debug"})
	require.NoError(t, err)
	require.NotNil(t, log)

	log.Info("console entry")
	require.NoError(t, closer.Close())
}

func TestNewPrettyConsole(t *testing.T) {
	log, closer, err := New(Options{Format: "pretty", NoColor: true})
	require.NoError(t, err)
	require.NotNil(t, log)

	log.Warn("pretty entry")
	require.NoError(t, closer.Close())
}

func TestNewFileJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	log, closer, err := New(Options{Output: "file", File: path})
	require.NoError(t, err)
	require.NotNil(t, log)

	log.WithMetadata(loglayer.M{"key": "value"}).Info("hello file")
	require.NoError(t, closer.Close())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(data, &entry), "file output must be one JSON object per line")
	assert.Equal(t, "hello file", entry["msg"])
	assert.Equal(t, "INFO", entry["level"])
}

func TestNewRejectsInvalidLevel(t *testing.T) {
	_, _, err := New(Options{Level: "loud"})
	assert.ErrorIs(t, err, ErrInvalidLevel)
}

func TestNewRejectsInvalidOutput(t *testing.T) {
	_, _, err := New(Options{Output: "syslog"})
	assert.ErrorIs(t, err, ErrInvalidOutput)
}

func TestNewRejectsInvalidFormat(t *testing.T) {
	_, _, err := New(Options{Format: "xml"})
	assert.ErrorIs(t, err, ErrInvalidFormat)
}

func TestNewRejectsPrettyOnFile(t *testing.T) {
	_, _, err := New(Options{
		Format: "pretty",
		Output: "file",
		File:   filepath.Join(t.TempDir(), "app.log"),
	})
	assert.ErrorIs(t, err, ErrPrettyRequiresConsole)
}

func TestNewFileRequiresPath(t *testing.T) {
	_, _, err := New(Options{Output: "file"})
	assert.ErrorIs(t, err, ErrFileRequired)
}

func TestIsTTY(t *testing.T) {
	// go test pipes stdout, so it is never a terminal here.
	assert.False(t, isTTY(os.Stdout))
}
