// Package appimage serves the application's branding images
// (logo light/dark, email logo, background, favicon, default profile
// picture). Images live in the blob store under "application-images/"
// with a per-name extension record kept in the DB; bundled defaults
// are seeded on startup and deletion is tracked with a marker object
// so a re-seed does not resurrect a removed image.
package appimage

import (
	"context"
	"errors"
	"io"
	"maps"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/riipandi/tango/internal/storage"
)

// Image names accepted by the API. logoLight/logoDark are selected by
// the ?light= query flag on the logo endpoints.
const (
	ImageLogoLight  = "logoLight"
	ImageLogoDark   = "logoDark"
	ImageEmailLogo  = "logoEmail"
	ImageBackground = "background"
	ImageFavicon    = "favicon"
	ImageProfilePic = "default-profile-picture"

	imagesPrefix  = "application-images"
	deletedPrefix = imagesPrefix + "/.deleted"
)

// errors mapped to HTTP by the transport layer.
var (
	ErrNotFound    = errors.New("appimage: image not found")
	ErrUnsupported = errors.New("appimage: unsupported file type")
	ErrInvalidName = errors.New("appimage: unknown image name")
	ErrTooLarge    = errors.New("appimage: file too large")
)

// mimeTypes is the upload/serve allowlist; anything else is rejected.
var mimeTypes = map[string]string{
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"svg":  "image/svg+xml",
	"ico":  "image/x-icon",
	"gif":  "image/gif",
	"webp": "image/webp",
	"avif": "image/avif",
	"heic": "image/heic",
}

// MimeForExt returns the allowed MIME type for ext or "".
func MimeForExt(ext string) string {
	return mimeTypes[strings.ToLower(ext)]
}

// Service reads and writes application images through the blob store.
// The extension map is process-local state seeded at startup; uploads
// and deletes keep it in sync under a mutex.
type Service struct {
	store      storage.Store
	mu         sync.RWMutex
	extensions map[string]string
}

// NewService builds the service and seeds bundled defaults.
func NewService(store storage.Store, defaults map[string]string) *Service {
	s := &Service{store: store, extensions: make(map[string]string, len(defaults))}
	maps.Copy(s.extensions, defaults)
	return s
}

// GetImage opens the stored image for name; ErrNotFound when unset.
func (s *Service) GetImage(ctx context.Context, name string) (io.ReadCloser, int64, string, error) {
	ext, ok := s.extension(name)
	if !ok {
		return nil, 0, "", ErrNotFound
	}
	mime := MimeForExt(ext)
	if mime == "" {
		return nil, 0, "", ErrUnsupported
	}
	reader, size, err := s.store.Open(ctx, imagePath(name, ext))
	if err != nil {
		if storage.IsNotExist(err) {
			return nil, 0, "", ErrNotFound
		}
		return nil, 0, "", err
	}
	return reader, size, mime, nil
}

// UpdateImage stores an upload under its extension, replacing any
// previous extension of the same image, and clears the delete marker.
func (s *Service) UpdateImage(ctx context.Context, name, filename string, r io.Reader) error {
	if !knownImage(name) {
		return ErrInvalidName
	}
	ext := strings.ToLower(path.Ext(filename))
	ext = strings.TrimPrefix(ext, ".")
	mime := MimeForExt(ext)
	if mime == "" {
		return ErrUnsupported
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.store.Save(ctx, imagePath(name, ext), r); err != nil {
		return err
	}
	if prev := s.extensions[name]; prev != "" && prev != ext {
		_ = s.store.Delete(ctx, imagePath(name, prev))
	}
	s.extensions[name] = ext
	_ = s.store.Delete(ctx, deletedPath(name))
	return nil
}

// DeleteImage removes the image and writes a tombstone so startup
// seeding does not restore it.
func (s *Service) DeleteImage(ctx context.Context, name string) error {
	if !knownImage(name) {
		return ErrInvalidName
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ext, ok := s.extensions[name]
	if !ok || ext == "" {
		return ErrNotFound
	}
	if err := s.store.Save(ctx, deletedPath(name), strings.NewReader("")); err != nil {
		return err
	}
	if err := s.store.Delete(ctx, imagePath(name, ext)); err != nil {
		return err
	}
	delete(s.extensions, name)
	return nil
}

// IsSet reports whether the image currently exists.
func (s *Service) IsSet(name string) bool {
	_, ok := s.extension(name)
	return ok
}

func (s *Service) extension(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ext, ok := s.extensions[name]
	return ext, ok && ext != ""
}

func knownImage(name string) bool {
	switch name {
	case ImageLogoLight, ImageLogoDark, ImageEmailLogo, ImageBackground, ImageFavicon, ImageProfilePic:
		return true
	}
	return false
}

func imagePath(name, ext string) string {
	return path.Join(imagesPrefix, name+"."+ext)
}

func deletedPath(name string) string {
	return path.Join(deletedPrefix, name)
}

// GetImageByPath is a test helper exposing raw-path reads.
func (s *Service) GetImageByPath(p string) (io.ReadCloser, int64, error) {
	r, size, err := s.store.Open(context.Background(), p)
	if err != nil {
		return nil, 0, err
	}
	return r, size, nil
}

// CacheControl is the value sent on image responses: 15 minutes fresh,
// then stale-while-revalidate for a day. skipCache=1 bypasses it.
func CacheControl(skip bool) string {
	if skip {
		return "no-cache"
	}
	return "public, max-age=900, stale-while-revalidate=86400"
}

// CacheTTL documents the freshness window used above.
const CacheTTL = 15 * time.Minute
