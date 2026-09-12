package logger

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.loglayer.dev/v3"
)

// TestQueueLogger proves the antree logger adapter: params become log
// fields on top of the application logger stack.
func TestQueueLogger(t *testing.T) {
	var buf bytes.Buffer

	core, err := loglayer.Build(loglayer.Config{
		Transport: slogTransport(
			slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo}),
			loglayer.LogLevelInfo,
		),
		Level: loglayer.LogLevelInfo,
	})
	require.NoError(t, err)

	queueLog := QueueLogger(core)
	queueLog.Info("task processed", "id", "abc", "attempt", 1)
	queueLog.Error("task failed", "queue", "email")

	lines := bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n"))
	require.Len(t, lines, 2)

	var info map[string]any
	require.NoError(t, json.Unmarshal(lines[0], &info))
	assert.Equal(t, "task processed", info["msg"])
	assert.Equal(t, "abc", info["id"])
	assert.Equal(t, float64(1), info["attempt"])

	var failure map[string]any
	require.NoError(t, json.Unmarshal(lines[1], &failure))
	assert.Equal(t, "task failed", failure["msg"])
	assert.Equal(t, "email", failure["queue"])
}
