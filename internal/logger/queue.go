package logger

import (
	"github.com/riipandi/tango/internal/antree"
	"go.loglayer.dev/v3"
)

// QueueLogger adapts the application logger to the antree queue logger
// interface. Params are alternating key/value pairs, mapped to log fields.
// The dependency points from the app to the antree package, never the
// other way around.
func QueueLogger(log Logger) antree.Logger {
	return antree.LoggerFunc(func(level, message string, params ...any) {
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
