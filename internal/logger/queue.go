package logger

import (
	"github.com/riipandi/tango/internal/queue"
	"go.loglayer.dev/v3"
)

// QueueLogger adapts the application logger to the queue logger interface.
func QueueLogger(log Logger) queue.Logger {
	return queue.LoggerFunc(func(level, message string, params ...any) {
		fields := loglayer.Fields{}
		for i := 0; i+1 < len(params); i += 2 {
			if key, ok := params[i].(string); ok {
				fields[key] = params[i+1]
			}
		}

		entry := log.WithFields(fields)
		if level == "error" {
			entry.Error(message)
			return
		}
		entry.Info(message)
	})
}
