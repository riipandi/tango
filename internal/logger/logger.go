package logger

import (
	"io"
	"log/slog"
	"os"
	"time"

	pretty "go.loglayer.dev/transports/pretty/v3"
	llslog "go.loglayer.dev/transports/slog/v3"
	"go.loglayer.dev/v3"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	// DefaultBufferSize is async queue capacity when Buffer unset.
	DefaultBufferSize = 4096

	// DefaultFlushTimeout bounds Close and close-on-fatal wait.
	DefaultFlushTimeout = 5 * time.Second
)

// File rotation for file output.
const (
	defaultMaxSizeMB   = 100
	defaultMaxBackups  = 7
	defaultMaxAgeDays  = 30
	defaultCompression = true
)

// New builds the logger: LogLayer API over an async transport.
// Structured emits JSON everywhere (stderr console, lumberjack
// file); pretty is colorized console-only. The closer drains the
// queue; call on shutdown (and before file reads in tests).
func New(opts Options) (Logger, io.Closer, error) {
	slogLevel, coreLevel, err := parseLevel(opts.Level)
	if err != nil {
		return nil, nil, err
	}

	inner, err := newTransport(opts, slogLevel, coreLevel)
	if err != nil {
		return nil, nil, err
	}

	asyncT := newAsync(inner, opts.Buffer, DefaultFlushTimeout)

	core, err := loglayer.Build(loglayer.Config{
		Transport:             asyncT,
		Level:                 coreLevel,
		TransportCloseTimeout: DefaultFlushTimeout,
	})
	if err != nil {
		return nil, nil, err
	}
	return core, asyncT, nil
}

// newTransport picks the sink behind the async queue.
func newTransport(opts Options, slogLevel slog.Level, coreLevel loglayer.LogLevel) (loglayer.Transport, error) {
	format := opts.Format
	if format == "" {
		format = "structured"
	}
	if format != "structured" && format != "pretty" {
		return nil, ErrInvalidFormat
	}

	switch opts.Output {
	case "", "console":
		if format == "pretty" {
			return pretty.New(pretty.Config{
				Writer:  os.Stdout,
				NoColor: opts.NoColor || !isTTY(os.Stdout),
				ID:      "pretty", Level: coreLevel,
			}), nil
		}
		return slogTransport(
			slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel}),
			coreLevel,
		), nil
	case "file":
		if format == "pretty" {
			return nil, ErrPrettyRequiresConsole
		}
		if opts.File == "" {
			return nil, ErrFileRequired
		}
		return slogTransport(
			slog.NewJSONHandler(&lumberjack.Logger{
				Filename:   opts.File,
				MaxSize:    defaultMaxSizeMB,
				MaxBackups: defaultMaxBackups,
				MaxAge:     defaultMaxAgeDays,
				Compress:   defaultCompression,
			}, &slog.HandlerOptions{Level: slogLevel}),
			coreLevel,
		), nil
	default:
		return nil, ErrInvalidOutput
	}
}

func slogTransport(h slog.Handler, coreLevel loglayer.LogLevel) loglayer.Transport {
	return llslog.New(llslog.Config{
		Logger: slog.New(h),
		ID:     "slog", Level: coreLevel,
	})
}

// isTTY reports whether f is a terminal.
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
