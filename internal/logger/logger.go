// Package logger wires the process's logging stack.
//
// One LogLayer core sits behind a *slog.Logger, so application code — and every
// dependency that logs through log/slog — emits into the same pipeline. The
// sinks are chosen by the configuration: a console renderer, an optional
// rotating file, and an optional OpenTelemetry exporter. Feature code never
// touches LogLayer directly; it calls slog, and this package decides where the
// entry goes.
package logger

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"

	"go.loglayer.dev/integrations/sloghandler/v3"
	"go.loglayer.dev/v3"

	"github.com/riipandi/tango/internal/config"
)

// Logger is the application logger: the LogLayer core behind it, the sinks it
// writes to, and the slog frontend the rest of the program calls.
//
// Every sink is opt-in except the console, so the default configuration writes to
// the terminal alone and a deployment adds a file or a collector by setting the
// key that names it.
type Logger struct {
	core *loglayer.LogLayer
	slog *slog.Logger

	// file and otlp are held so Shutdown can release them: the file sink owns a
	// descriptor, and the collector owns a queue nothing else can flush.
	file *fileSink
	otlp *otlpSink
}

// Options carries what a caller overrides at construction. Everything else comes
// from the resolved configuration, which is the single source of truth.
type Options struct {
	// Writer is the console destination. It defaults to stdout, which is where a
	// service logs; a test passes a buffer to read what was emitted.
	Writer io.Writer
}

// Option adjusts Options.
type Option func(*Options)

// WithWriter sends the console rendering somewhere other than stdout.
func WithWriter(w io.Writer) Option {
	return func(o *Options) { o.Writer = w }
}

// New builds the logger the configuration describes.
//
// It fails rather than degrading: a sink that cannot be built is an error, not a
// silent downgrade to the console, because a deployment that asked for a file or
// a collector and did not get one has lost the logs it is being trusted with.
// Nothing is left open when it fails part way through.
func New(cfg config.Config, opts ...Option) (*Logger, error) {
	options := Options{Writer: os.Stdout}
	for _, apply := range opts {
		apply(&options)
	}

	l := &Logger{}
	transports := []loglayer.Transport{consoleTransport(cfg, options.Writer)}

	if cfg.Log.File.Filename != "" {
		file, err := newFileSink(cfg.Log.File)
		if err != nil {
			return nil, errors.Join(err, l.release())
		}
		l.file = file
		transports = append(transports, file.transport)
	}

	if cfg.Log.OTLP.Enable {
		otlp, err := newOTLPSink(cfg)
		if err != nil {
			return nil, errors.Join(err, l.release())
		}
		l.otlp = otlp
		transports = append(transports, otlp.transport)
	}

	core, err := loglayer.Build(loglayer.Config{
		Transports: transports,
		Level:      levelFor(cfg.Log.Level),
		// Persistent fields and per-call metadata merge at the root rather than
		// nesting under one key, so an entry reads the way slog's own JSON
		// handler writes it. slog is the frontend every caller uses, so its
		// shape is the one to match.
		FlattenMetadata: true,
	})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("logger: %w", err), l.release())
	}
	l.core = core
	l.slog = slog.New(sloghandler.New(core))
	return l, nil
}

// Slog returns the logger the application calls.
func (l *Logger) Slog() *slog.Logger { return l.slog }

// Log returns the LogLayer core, for the few places that need a LogLayer-only
// feature such as a plugin. Feature code logs through Slog.
func (l *Logger) Log() *loglayer.LogLayer { return l.core }

// SetDefault installs the logger as the process default, so a package that logs
// through slog without being handed one lands in the same pipeline.
func (l *Logger) SetDefault() { slog.SetDefault(l.slog) }

// Shutdown flushes and releases every sink. It is safe to call more than once,
// and a second call does nothing.
//
// The collector is drained with the caller's context: its batch processor holds
// queued records that nothing else can flush, so a shutdown that skips it loses
// whatever was queued. The file sink needs no drain, only its descriptor back.
func (l *Logger) Shutdown(ctx context.Context) error {
	var errs []error
	if l.otlp != nil {
		if err := l.otlp.shutdown(ctx); err != nil {
			errs = append(errs, err)
		}
		l.otlp = nil
	}
	if l.file != nil {
		if err := l.file.close(); err != nil {
			errs = append(errs, err)
		}
		l.file = nil
	}
	return errors.Join(errs...)
}

// release closes every sink that was built before a later one failed. The sinks
// are already unusable, so a failure here is secondary to the one that stopped
// construction and is joined rather than raised.
func (l *Logger) release() error {
	return l.Shutdown(context.Background())
}
