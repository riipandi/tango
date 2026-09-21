package logger

import (
	"fmt"

	"go.loglayer.dev/transports/lumberjack/v3"

	"github.com/riipandi/tango/internal/config"
)

// fileSink is the rotating file transport, held so Shutdown can release the file
// descriptor it owns.
type fileSink struct {
	transport *lumberjack.Transport
}

// newFileSink builds the rotating file sink the configuration names.
//
// Rotation settings are passed through as configured; Validate has already
// refused the combination that never deletes a rotated file.
func newFileSink(cfg config.LogFile) (*fileSink, error) {
	transport, err := lumberjack.Build(lumberjack.Config{
		Filename:   cfg.Filename,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		Compress:   cfg.Compress,
	})
	if err != nil {
		return nil, fmt.Errorf("logger: file sink: %w", err)
	}
	return &fileSink{transport: transport}, nil
}

// close releases the file handle. The sink stops accepting entries first, so a
// late call cannot make the rotator reopen the file it just closed.
func (s *fileSink) close() error {
	return s.transport.Close()
}
