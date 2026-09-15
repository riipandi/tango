package queue

// Logger logs queue operations.
type Logger interface {
	Info(message string, params ...any)
	Error(message string, params ...any)
}

// LoggerFunc adapts functions with a different call shape.
type LoggerFunc func(level string, message string, params ...any)

func (f LoggerFunc) Info(message string, params ...any)  { f("info", message, params...) }
func (f LoggerFunc) Error(message string, params ...any) { f("error", message, params...) }

// noLogger discards log entries.
type noLogger struct{}

func (noLogger) Info(string, ...any)  {}
func (noLogger) Error(string, ...any) {}
