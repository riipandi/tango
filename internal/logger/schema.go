// Package logger wires LogLayer to console or file sinks.
package logger

import (
	"errors"

	"go.loglayer.dev/v3"
)

// Logger is the application logging contract.
type Logger = *loglayer.LogLayer

// NewMock returns a silent logger for tests and optional components.
func NewMock() Logger {
	return loglayer.NewMock()
}

// Errors returned by New.
var (
	// ErrInvalidLevel reports an unknown level.
	ErrInvalidLevel = errors.New("logger: invalid level (want trace|debug|info|warn|error|fatal|panic)")

	// ErrInvalidOutput reports an unknown output.
	ErrInvalidOutput = errors.New("logger: invalid output (want console|file)")

	// ErrInvalidFormat reports an unknown format.
	ErrInvalidFormat = errors.New("logger: invalid format (want pretty|structured)")

	// ErrPrettyRequiresConsole reports pretty output requested for a file.
	ErrPrettyRequiresConsole = errors.New("logger: pretty format requires console output")

	// ErrFileRequired reports file output without a path.
	ErrFileRequired = errors.New("logger: file output requires a file path")
)

// Options selects the sink and format for New.
type Options struct {
	// Level accepts trace, debug, info, warn, error, fatal, or panic.
	Level string

	// Output: "console" (default) or "file".
	Output string

	// Format accepts structured (JSON) or pretty (console only).
	Format string

	// NoColor disables colors in pretty output.
	NoColor bool

	// File is the path for file output.
	File string

	// Buffer is async queue capacity. Zero uses DefaultBufferSize.
	Buffer int
}
