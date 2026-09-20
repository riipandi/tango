package health

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// CheckNameStorage is the name of the data directory check in a result.
const CheckNameStorage = "storage"

// DefaultStorageTimeout bounds the data directory probe.
const DefaultStorageTimeout = 2 * time.Second

// StorageCheck reports whether the application data directory can be used.
//
// dir is resolved to an absolute path before the probe, and the result reports
// that path. A relative path in the report would not say which directory was
// inspected, because it depends on the working directory of the process that
// happened to run the check.
//
// It fails when the directory is missing, is not a directory, cannot be written
// to, or is world-writable.
//
// Writability is tested by writing and removing a temporary file. Mode bits
// alone are not enough: an ACL, a read-only mount, or a full disk lets a
// directory that looks writable reject the write, and the application would
// only find out when it tried to store something.
//
// World-writable is a failure rather than a note, because this directory holds
// uploads and certificates, so any local user could replace them.
func StorageCheck(dir string) Check {
	resolved := absolutePath(dir)
	return Check{
		Name:    CheckNameStorage,
		Target:  resolved,
		Timeout: DefaultStorageTimeout,
		Check: func(context.Context) error {
			return checkDataDir(resolved)
		},
	}
}

// absolutePath resolves dir against the working directory. It keeps the input
// when the working directory cannot be read, so the check still reports a path
// instead of an empty target.
func absolutePath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir
	}
	return abs
}

// checkDataDir returns the first problem that makes the directory unusable.
func checkDataDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("storage: directory does not exist: %s", dir)
		}
		return fmt.Errorf("storage: stat %s: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("storage: not a directory: %s", dir)
	}
	if mode := info.Mode().Perm(); mode&0o002 != 0 {
		return fmt.Errorf("storage: directory mode %04o is world-writable: %s", mode, dir)
	}
	return probeWritable(dir)
}

// probeWritable writes and removes a temporary file. The file is removed on
// every path, so a failed probe does not leave anything behind.
func probeWritable(dir string) error {
	probe, err := os.CreateTemp(dir, ".health-write-probe-*")
	if err != nil {
		return fmt.Errorf("storage: directory is not writable: %s: %w", dir, err)
	}

	name := probe.Name()
	if err := probe.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("storage: close write probe: %w", err)
	}
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("storage: remove write probe %s: %w", name, err)
	}
	return nil
}
