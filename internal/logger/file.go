package logger

import (
	"fmt"
	"path/filepath"

	"go.loglayer.dev/transports/lumberjack/v3"
	lltransport "go.loglayer.dev/v3/transport"

	"github.com/riipandi/tango/internal/config"
)

// fileSink is the rotating file transport, held so Shutdown can release the file
// descriptor it owns.
type fileSink struct {
	transport *lumberjack.Transport
}

// newFileSink builds the rotating file sink the configuration names.
//
// The path is derived from storage.local_path rather than configured on its own:
// there is one data directory in this application, and a second path for the logs
// could disagree with it. A relative path in the configuration stays relative, so
// the sink writes where the working directory says, exactly as the local storage
// driver does.
//
// Rotation settings are passed through as configured; Validate has already
// refused the combination that never deletes a rotated file.
func newFileSink(cfg config.Config) (*fileSink, error) {
	rotator, err := lumberjack.Build(lumberjack.Config{
		Filename:   LogFilePath(cfg),
		MaxSize:    cfg.Log.File.MaxSize,
		MaxBackups: cfg.Log.File.MaxBackups,
		MaxAge:     cfg.Log.File.MaxAge,
		Compress:   cfg.Log.File.Compress,
		BaseConfig: lltransport.BaseConfig{ID: "file"},
	})
	if err != nil {
		return nil, fmt.Errorf("logger: file sink: %w", err)
	}
	return &fileSink{transport: rotator}, nil
}

// LogFilePath is the active log file, under the one data directory of the
// process. It is exported because a command that reports where logs go — or a
// test that reads them — needs the same answer the sink uses, and re-deriving it
// at the call site is how two paths start to disagree.
func LogFilePath(cfg config.Config) string {
	return filepath.Join(dataDir(cfg), config.LogDir, config.LogFileName)
}

// dataDir resolves the application data directory from the configuration. It is
// Storage.LocalPath, the one path the local driver writes to; the layer already
// applied the precedence, so there is nothing to re-check here.
func dataDir(cfg config.Config) string {
	if cfg.Storage.LocalPath == "" {
		return config.DefaultDataDir
	}
	return cfg.Storage.LocalPath
}

// close releases the file handle. The sink stops accepting entries first, so a
// late call cannot make the rotator reopen the file it just closed.
func (s *fileSink) close() error {
	return s.transport.Close()
}
