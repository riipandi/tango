// Package logger wires the LogLayer fluent API to the configured
// sinks: a colorized pretty console (LogLayer pretty renderer) or
// always-structured JSON — slog JSON handlers, plain on stderr or
// rotating over lumberjack. Dispatch is async and non-blocking —
// emission never waits on I/O; overflowing entries are dropped and
// counted.
//
// Config keys (see internal/config): app.log_level, app.log_transport
// (console|file), app.log_pretty, app.log_file. Future log backends
// (HTTP shippers, cloud services) slot in as additional transports
// inside New without touching callers.
package logger

import (
	"errors"

	"go.loglayer.dev/v3"
)

// Logger is the application logging contract. It is a type alias
// over the LogLayer fluent API so modules get the full surface
// (levels, fields, metadata, errors) while keeping the concrete
// implementation swappable in this package.
type Logger = *loglayer.LogLayer

// Errors returned by New.
var (
	// ErrInvalidLevel is returned for an unknown level string.
	ErrInvalidLevel = errors.New("logger: invalid level (want trace|debug|info|warn|error|fatal|panic)")

	// ErrInvalidOutput is returned for an unknown output selection.
	ErrInvalidOutput = errors.New("logger: invalid output (want console|file)")

	// ErrInvalidFormat is returned for an unknown format string.
	ErrInvalidFormat = errors.New("logger: invalid format (want pretty|structured)")

	// ErrPrettyRequiresConsole is returned when the pretty format
	// is combined with file output; files are always structured.
	ErrPrettyRequiresConsole = errors.New("logger: pretty format requires console output")

	// ErrFileRequired is returned when output is file without a path.
	ErrFileRequired = errors.New("logger: file output requires a file path")
)

// Options selects the sinks and rendering for New. It mirrors the
// app.log_* config keys; passing a zero value yields the defaults
// (info level, structured JSON on console).
type Options struct {
	// Level is the minimum level: trace, debug, info, warn, error,
	// fatal, or panic. Empty means info.
	Level string

	// Output is "console" (default) or "file".
	Output string

	// Format is "structured" (default, JSON everywhere) or
	// "pretty" (colorized renderer, console only).
	Format string

	// NoColor forces ANSI colors off for pretty output. When left
	// false, colors are auto-disabled when stdout is not a terminal.
	NoColor bool

	// File is the log file path for file output. Rotation defaults
	// apply (100 MB, 7 backups, 30 days, gzip).
	File string

	// Buffer is the async queue capacity. Zero uses
	// DefaultBufferSize.
	Buffer int
}
