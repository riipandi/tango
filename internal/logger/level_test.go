package logger

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.loglayer.dev/v3"
)

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		slogLvl slog.Level
		coreLvl loglayer.LogLevel
		wantErr error
	}{
		{"trace", slog.LevelDebug, loglayer.LogLevelTrace, nil},
		{"debug", slog.LevelDebug, loglayer.LogLevelDebug, nil},
		{"", slog.LevelInfo, loglayer.LogLevelInfo, nil},
		{"info", slog.LevelInfo, loglayer.LogLevelInfo, nil},
		{"INFO", slog.LevelInfo, loglayer.LogLevelInfo, nil},
		{"warn", slog.LevelWarn, loglayer.LogLevelWarn, nil},
		{"warning", slog.LevelWarn, loglayer.LogLevelWarn, nil},
		{"error", slog.LevelError, loglayer.LogLevelError, nil},
		{"fatal", slog.LevelError, loglayer.LogLevelFatal, nil},
		{"panic", slog.LevelError, loglayer.LogLevelPanic, nil},
		{"nope", 0, 0, ErrInvalidLevel},
	}

	for _, tc := range cases {
		slogLvl, coreLvl, err := parseLevel(tc.in)
		if tc.wantErr != nil {
			assert.ErrorIs(t, err, tc.wantErr, "level %q", tc.in)
			continue
		}
		assert.NoError(t, err, "level %q", tc.in)
		assert.Equal(t, tc.slogLvl, slogLvl, "slog level for %q", tc.in)
		assert.Equal(t, tc.coreLvl, coreLvl, "core level for %q", tc.in)
	}
}
