package storage

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	"github.com/riipandi/tango/internal/config"
)

// Backend type names.
const (
	TypeFilesystem = "filesystem"
	TypeS3         = "s3"
)

// ErrNotFound reports a missing object.
var ErrNotFound = errors.New("storage: object not found")

// IsNotExist reports whether err came from a missing object.
func IsNotExist(err error) bool {
	return errors.Is(err, ErrNotFound) || errors.Is(err, fs.ErrNotExist) || os.IsNotExist(err)
}

// ObjectInfo describes one stored object in a List result.
type ObjectInfo struct {
	Path    string
	Size    int64
	ModTime os.FileInfo // nil for S3 listings without the field
}

// Store is the blob backend contract. Paths are root-relative and use slashes.
type Store interface {
	Save(ctx context.Context, path string, data io.Reader) error
	Open(ctx context.Context, path string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, path string) error
	DeleteAll(ctx context.Context, prefix string) error
	List(ctx context.Context, prefix string) ([]ObjectInfo, error)
	Type() string
}

// New selects S3 when an endpoint is set, otherwise filesystem storage.
func New(cfg config.StorageConfig) (Store, error) {
	if cfg.S3EndpointURL != "" {
		return NewS3Storage(S3Config{
			Bucket:          cfg.S3BucketDefault,
			Region:          cfg.S3Region,
			Endpoint:        cfg.S3EndpointURL,
			AccessKeyID:     cfg.S3AccessKeyID,
			SecretAccessKey: cfg.S3SecretAccessKey,
			ForcePathStyle:  cfg.S3ForcePathStyle,
			Root:            deref(cfg.S3PathPrefix),
		})
	}
	return NewFilesystemStorage(cfg.DataDir)
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.Trim(*p, "/")
}
