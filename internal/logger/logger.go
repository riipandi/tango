package logger

import (
	"context"
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
	// DefaultBufferSize is the async queue capacity when unset.
	DefaultBufferSize = 4096

	// DefaultFlushTimeout bounds shutdown and fatal-log flushing.
	DefaultFlushTimeout = 5 * time.Second
)

// File rotation defaults.
const (
	defaultMaxSizeMB   = 100
	defaultMaxBackups  = 7
	defaultMaxAgeDays  = 30
	defaultCompression = true
)

// New builds the logger over an asynchronous transport.
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

// newTransport picks the sink behind the queue.
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

// Slog adapts the shared logger to slog.Logger.
func Slog(log Logger) *slog.Logger {
	return slog.New(&loglayerHandler{log: log})
}

// loglayerHandler forwards slog records into loglayer.
type loglayerHandler struct {
	log   Logger
	attrs []slog.Attr
}

func (h *loglayerHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *loglayerHandler) Handle(_ context.Context, r slog.Record) error {
	fields := loglayer.Fields{}
	for _, a := range h.attrs {
		fields[a.Key] = a.Value.Any()
	}
	r.Attrs(func(a slog.Attr) bool {
		fields[a.Key] = a.Value.Any()
		return true
	})
	switch r.Level {
	case slog.LevelError:
		h.log.Error(r.Message, fields)
	case slog.LevelWarn:
		h.log.Warn(r.Message, fields)
	case slog.LevelDebug:
		h.log.Debug(r.Message, fields)
	default:
		h.log.Info(r.Message, fields)
	}
	return nil
}

func (h *loglayerHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &loglayerHandler{log: h.log, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *loglayerHandler) WithGroup(string) slog.Handler { return h }

// isTTY reports whether f is a terminal.
func isTTY(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
