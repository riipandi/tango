package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// filesystemStorage keeps every object under a fixed root via os.Root,
// so path escapes ("..", absolute) are impossible by construction.
type filesystemStorage struct {
	root             *os.Root
	absoluteRootPath string
}

// NewFilesystemStorage builds the disk backend rooted at rootPath.
func NewFilesystemStorage(rootPath string) (Store, error) {
	if err := os.MkdirAll(rootPath, 0o700); err != nil {
		return nil, fmt.Errorf("storage: create root %q: %w", rootPath, err)
	}
	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("storage: open root %q: %w", rootPath, err)
	}
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve root %q: %w", rootPath, err)
	}
	return &filesystemStorage{root: root, absoluteRootPath: abs}, nil
}

func (s *filesystemStorage) Type() string { return TypeFilesystem }

func (s *filesystemStorage) Save(_ context.Context, path string, data io.Reader) error {
	path = filepath.FromSlash(path)
	if err := s.root.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("storage: mkdir for %q: %w", path, err)
	}

	// Write to a temp sibling then rename: readers never observe a
	// partially written object.
	tmp := path + ".tmp"
	f, err := s.root.Create(tmp)
	if err != nil {
		return fmt.Errorf("storage: create temp for %q: %w", path, err)
	}
	if _, err = io.Copy(f, data); err != nil {
		f.Close()
		_ = s.root.Remove(tmp)
		return fmt.Errorf("storage: write %q: %w", path, err)
	}
	if err = f.Close(); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("storage: close %q: %w", path, err)
	}
	if err = s.root.Rename(tmp, path); err != nil {
		_ = s.root.Remove(tmp)
		return fmt.Errorf("storage: rename to %q: %w", path, err)
	}
	return nil
}

func (s *filesystemStorage) Open(_ context.Context, path string) (io.ReadCloser, int64, error) {
	f, err := s.root.Open(filepath.FromSlash(path))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, 0, ErrNotFound
		}
		return nil, 0, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, 0, err
	}
	return f, info.Size(), nil
}

func (s *filesystemStorage) Delete(_ context.Context, path string) error {
	err := s.root.Remove(filepath.FromSlash(path))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *filesystemStorage) DeleteAll(_ context.Context, prefix string) error {
	prefix = strings.Trim(filepath.FromSlash(prefix), "/")
	if prefix == "" || prefix == "." {
		return fmt.Errorf("storage: refusing to delete whole root")
	}
	err := s.root.RemoveAll(prefix)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (s *filesystemStorage) List(_ context.Context, prefix string) ([]ObjectInfo, error) {
	prefix = strings.Trim(filepath.FromSlash(prefix), "/")
	var objects []ObjectInfo
	// Recursive walk so FS and S3 listings share one contract.
	walkErr := filepath.WalkDir(filepath.Join(s.absoluteRootPath, filepath.FromSlash(prefix)),
		func(full string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(s.absoluteRootPath, full)
			if err != nil {
				return err
			}
			objects = append(objects, ObjectInfo{Path: filepath.ToSlash(rel), Size: info.Size(), ModTime: info})
			return nil
		})
	if walkErr != nil {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, walkErr
	}
	return objects, nil
}
