package antree

// Logger is used to log queue operations. log/slog.Logger satisfies this
// interface as-is; adapt any other logger with LoggerFunc.
type Logger interface {
	// Info logs info messages. Params are alternating key/value pairs.
	Info(message string, params ...any)

	// Error logs error messages. Params are alternating key/value pairs.
	Error(message string, params ...any)
}

// LoggerFunc adapts individual log functions to Logger, so loggers with a
// different call shape (loglayer, zap, ...) integrate without this package
// importing them.
type LoggerFunc func(level string, message string, params ...any)

func (f LoggerFunc) Info(message string, params ...any)  { f("info", message, params...) }
func (f LoggerFunc) Error(message string, params ...any) { f("error", message, params...) }

// noLogger is the default logger and logs nothing.
type noLogger struct{}

func (noLogger) Info(string, ...any)  {}
func (noLogger) Error(string, ...any) {}
