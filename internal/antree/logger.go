package antree

// Logger logs queue operations. log/slog.Logger satisfies this as-is;
// adapt any other logger with LoggerFunc. Params are alternating key/value
// pairs.
type Logger interface {
	Info(message string, params ...any)
	Error(message string, params ...any)
}

// LoggerFunc adapts log functions with a different call shape (loglayer,
// zap, ...) without this package importing them.
type LoggerFunc func(level string, message string, params ...any)

func (f LoggerFunc) Info(message string, params ...any)  { f("info", message, params...) }
func (f LoggerFunc) Error(message string, params ...any) { f("error", message, params...) }

// noLogger is the default logger and logs nothing.
type noLogger struct{}

func (noLogger) Info(string, ...any)  {}
func (noLogger) Error(string, ...any) {}
