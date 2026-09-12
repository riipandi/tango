package logger

import (
	"log/slog"
	"strings"

	"go.loglayer.dev/v3"
)

// parseLevel maps a string to slog handler level and core level.
// Slog lacks trace/fatal/panic: trace→debug, fatal/panic→error at
// the handler; core keeps the true level (fatal exits, panic
// panics). Empty means info.
func parseLevel(s string) (slog.Level, loglayer.LogLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return slog.LevelDebug, loglayer.LogLevelTrace, nil
	case "debug":
		return slog.LevelDebug, loglayer.LogLevelDebug, nil
	case "", "info":
		return slog.LevelInfo, loglayer.LogLevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, loglayer.LogLevelWarn, nil
	case "error":
		return slog.LevelError, loglayer.LogLevelError, nil
	case "fatal":
		return slog.LevelError, loglayer.LogLevelFatal, nil
	case "panic":
		return slog.LevelError, loglayer.LogLevelPanic, nil
	default:
		return 0, 0, ErrInvalidLevel
	}
}
