// Package logger wires the LogLayer API to sinks: pretty console
// or structured JSON (stderr, or rotating file via lumberjack).
// Dispatch is async and non-blocking; overflow drops and counts.
//
// Config keys: app.log_level, app.log_transport (console|file),
// app.log_format, app.log_file. New backends slot into New.
package logger

import (
	"errors"

	"go.loglayer.dev/v3"
)

// Logger is the app logging contract: alias over LogLayer, full
// surface, implementation swappable here.
type Logger = *loglayer.LogLayer

// NewMock is a silent logger with the same API; Fatal doesn't exit.
// For tests and optional components.
func NewMock() Logger {
	return loglayer.NewMock()
}

// Errors returned by New.
var (
	// ErrInvalidLevel for unknown level strings.
	ErrInvalidLevel = errors.New("logger: invalid level (want trace|debug|info|warn|error|fatal|panic)")

	// ErrInvalidOutput for unknown output selections.
	ErrInvalidOutput = errors.New("logger: invalid output (want console|file)")

	// ErrInvalidFormat for unknown format strings.
	ErrInvalidFormat = errors.New("logger: invalid format (want pretty|structured)")

	// ErrPrettyRequiresConsole: pretty is console-only, files stay structured.
	ErrPrettyRequiresConsole = errors.New("logger: pretty format requires console output")

	// ErrFileRequired when file output lacks a path.
	ErrFileRequired = errors.New("logger: file output requires a file path")
)

// Options selects sinks and rendering for New. Mirrors app.log_*
// keys; zero value is info, structured JSON on console.
type Options struct {
	// Level: trace, debug, info, warn, error, fatal, panic.
	// Empty means info.
	Level string

	// Output: "console" (default) or "file".
	Output string

	// Format: "structured" (default, JSON) or "pretty"
	// (colorized, console only).
	Format string

	// NoColor forces pretty colors off. False auto-disables
	// when stdout isn't a terminal.
	NoColor bool

	// File path for file output. Rotation: 100MB, 7 backups,
	// 30 days, gzip.
	File string

	// Buffer is async queue capacity. Zero uses DefaultBufferSize.
	Buffer int
}
