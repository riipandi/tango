package appimage

import (
	"context"
	"os"
	"path"
	"strings"

	"github.com/riipandi/tango/internal/storage"
)

// SeedDefaults copies bundled images into the blob store and returns
// the detected extension map. Existing images and delete markers win.
func SeedDefaults(ctx context.Context, store storage.Store, sourceDir string) (map[string]string, error) {
	entries, err := os.ReadDir(sourceDir)
	if os.IsNotExist(err) {
		// No generated assets (e.g. tests): nothing to seed.
		return map[string]string{}, nil
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
		if MimeForExt(ext) == "" {
			continue
		}
		deleted, tombErr := objectExists(ctx, store, deletedPath(name))
		if tombErr != nil {
			return nil, tombErr
		}
		if deleted {
			continue
		}
		src, openErr := os.Open(path.Join(sourceDir, entry.Name()))
		if openErr != nil {
			return nil, openErr
		}
		saveErr := store.Save(ctx, imagePath(name, ext), src)
		src.Close()
		if saveErr != nil {
			return nil, saveErr
		}
		extensions[name] = ext
	}

	// Keep extensions for images already in the store (previous boot
	// may have stored custom uploads not in the bundle).
	stored, err := store.List(ctx, imagesPrefix)
	if err != nil && !storage.IsNotExist(err) {
		return nil, err
	}
	for _, obj := range stored {
		if strings.HasPrefix(obj.Path, deletedPrefix+"/") {
			continue
		}
		name, ext := splitName(path.Base(obj.Path))
		if extensions[name] == "" {
			extensions[name] = ext
		}
	}
	return extensions, nil
}

func splitName(full string) (name, ext string) {
	dot := strings.LastIndex(full, ".")
	if dot <= 0 {
		return full, ""
	}
	return full[:dot], full[dot+1:]
}

func objectExists(ctx context.Context, store storage.Store, p string) (bool, error) {
	reader, _, err := store.Open(ctx, p)
	if err == nil {
		reader.Close()
		return true, nil
	}
	if storage.IsNotExist(err) {
		return false, nil
	}
	return false, err
}
