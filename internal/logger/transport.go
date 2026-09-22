package logger

import (
	"io"

	"go.loglayer.dev/transports/pretty/v3"
	"go.loglayer.dev/transports/structured/v3"
	"go.loglayer.dev/v3"
	"go.loglayer.dev/v3/transport"

	"github.com/riipandi/tango/internal/config"
)

// consoleTransport builds the sink a human reads: the colourised renderer on a
// terminal, or one JSON object per line when the format asks for it.
//
// The console is the only sink with a rendering choice. A file or a collector
// keeps its own form, because what a person reads in a terminal and what a
// machine reads from a log file are two different questions, and answering the
// second with the first would put escape codes in the file.
func consoleTransport(cfg config.Config, w io.Writer) loglayer.Transport {
	if cfg.Log.Console.Format == config.LogStructured {
		return structured.New(structured.Config{
			Writer:     w,
			BaseConfig: transport.BaseConfig{ID: "console"},
		})
	}
	return pretty.New(pretty.Config{
		Writer:     w,
		BaseConfig: transport.BaseConfig{ID: "console"},
		// The renderer decides colour per destination, like pkg/printext: a
		// redirected run and a test buffer must stay plain text.
		NoColor: !isTerminal(w),
	})
}

// levelFor maps a configured level onto the logger's own threshold. The
// configuration has already rejected anything else, so an unknown value is not
// reachable; it is treated as info rather than as "every level", because a
// logger that suddenly emits debug lines is worse than a quiet one.
func levelFor(level string) loglayer.LogLevel {
	switch level {
	case config.LogDebug:
		return loglayer.LogLevelDebug
	case config.LogWarn:
		return loglayer.LogLevelWarn
	case config.LogError:
		return loglayer.LogLevelError
	case config.LogInfo:
		return loglayer.LogLevelInfo
	default:
		return loglayer.LogLevelInfo
	}
}
