package appimage

import (
	"bytes"
	"strings"
	"testing"

	"github.com/riipandi/tango/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImageLifecycle(t *testing.T) {
	store, err := storage.NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)
	svc := NewService(store, nil)

	// Unknown name rejected.
	err = svc.UpdateImage(t.Context(), "bogus", "x.png", strings.NewReader("data"))
	assert.ErrorIs(t, err, ErrInvalidName)

	// Upload favicon, then read it back.
	require.NoError(t, svc.UpdateImage(t.Context(), ImageFavicon, "favicon.ico", strings.NewReader("ico-data")))
	reader, size, mime, err := svc.GetImage(t.Context(), ImageFavicon)
	require.NoError(t, err)
	contents, err := readAll(reader)
	require.NoError(t, err)
	assert.Equal(t, "ico-data", string(contents))
	assert.Equal(t, int64(len(contents)), size)
	assert.Equal(t, "image/x-icon", mime)
	assert.True(t, svc.IsSet(ImageFavicon))

	require.NoError(t, svc.UpdateImage(t.Context(), ImageFavicon, "favicon.png", strings.NewReader("png-data")))
	_, _, mime, err = svc.GetImage(t.Context(), ImageFavicon)
	require.NoError(t, err)
	assert.Equal(t, "image/png", mime)
	_, _, err = svc.GetImageByPath(imagePath(ImageFavicon, "ico"))
	assert.ErrorIs(t, err, storage.ErrNotFound)

	// Unsupported type rejected.
	err = svc.UpdateImage(t.Context(), ImageBackground, "background.txt", strings.NewReader("no"))
	assert.ErrorIs(t, err, ErrUnsupported)

	// Delete tombstones the image; re-seed skips it.
	require.NoError(t, svc.DeleteImage(t.Context(), ImageFavicon))
	_, _, _, err = svc.GetImage(t.Context(), ImageFavicon)
	assert.ErrorIs(t, err, ErrNotFound)
	assert.False(t, svc.IsSet(ImageFavicon))

	// SeedDefaults honors the tombstone.
	defaults, err := SeedDefaults(t.Context(), store, "testdata/images")
	require.NoError(t, err)
	_, ok := defaults[ImageFavicon]
	assert.False(t, ok, "tombstoned image must not be re-seeded")
}

func TestSeedDefaults(t *testing.T) {
	store, err := storage.NewFilesystemStorage(t.TempDir())
	require.NoError(t, err)

	defaults, err := SeedDefaults(t.Context(), store, "testdata/images")
	require.NoError(t, err)
	assert.Equal(t, "jpg", defaults[ImageBackground])
	assert.Equal(t, "svg", defaults[ImageEmailLogo])

	svc := NewService(store, defaults)
	_, _, mime, err := svc.GetImage(t.Context(), ImageBackground)
	require.NoError(t, err)
	assert.Equal(t, "image/jpeg", mime)
}

func readAll(r interface{ Read([]byte) (int, error) }) ([]byte, error) {
	var buf bytes.Buffer
	tmp := make([]byte, 512)
	for {
		n, err := r.Read(tmp)
		buf.Write(tmp[:n])
		if err != nil {
			if err.Error() == "EOF" {
				return buf.Bytes(), nil
			}
			return buf.Bytes(), err
		}
	}
}
