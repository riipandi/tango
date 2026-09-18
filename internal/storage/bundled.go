package storage

import (
	"context"
	"io"
	"os"
	"path"
	"strings"
)

// DefaultProfilePicture is the bundled fallback image served when a
// user has no custom profile picture.
const DefaultProfilePicture = "default-profile-picture"

// bundledPrefix roots seeded images in the blob store.
const bundledPrefix = "application-images"

// bundledMime maps bundled file extensions to MIME types; anything
// else is skipped during seeding.
var bundledMime = map[string]string{
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

// BundledImages is the read-only view of images seeded from the
// frontend asset bundle into the blob store.
type BundledImages struct {
	store      Store
	extensions map[string]string
}

// SeedBundledImages copies bundled images from sourceDir into the
// blob store and returns the seeded set. A missing sourceDir (fresh
// checkouts, tests) seeds nothing.
func SeedBundledImages(ctx context.Context, store Store, sourceDir string) (*BundledImages, error) {
	entries, err := os.ReadDir(sourceDir)
	if os.IsNotExist(err) {
		return &BundledImages{store: store, extensions: map[string]string{}}, nil
	}
	if err != nil {
		return nil, err
	}

	extensions := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name, ext := splitName(entry.Name())
		if bundledMime[strings.ToLower(ext)] == "" {
			continue
		}
		src, openErr := os.Open(path.Join(sourceDir, entry.Name()))
		if openErr != nil {
			return nil, openErr
		}
		saveErr := store.Save(ctx, bundledPath(name, ext), src)
		src.Close()
		if saveErr != nil {
			return nil, saveErr
		}
		extensions[name] = strings.ToLower(ext)
	}
	return &BundledImages{store: store, extensions: extensions}, nil
}

// Open returns the bundled image bytes, size, and MIME type;
// ok=false when the image is not seeded.
func (b *BundledImages) Open(ctx context.Context, name string) (io.ReadCloser, int64, string, bool) {
	ext, ok := b.extensions[name]
	if !ok {
		return nil, 0, "", false
	}
	reader, size, err := b.store.Open(ctx, bundledPath(name, ext))
	if err != nil {
		return nil, 0, "", false
	}
	return reader, size, bundledMime[ext], true
}

func bundledPath(name, ext string) string {
	return path.Join(bundledPrefix, name+"."+ext)
}

func splitName(full string) (name, ext string) {
	dot := strings.LastIndex(full, ".")
	if dot <= 0 {
		return full, ""
	}
	return full[:dot], full[dot+1:]
}
