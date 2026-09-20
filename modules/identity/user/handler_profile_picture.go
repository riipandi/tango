package user

// profile_picture.go serves self/admin upload and reset, plus the
// public .png read. Uploaded files are stored as-is behind a MIME
// allowlist. Missing custom pictures fall back to the bundled
// default.

import (
	"io"
	"net/http"
	"path"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// maxPictureUpload bounds one profile picture upload.
const maxPictureUpload = 5 << 20 // 5 MiB

// pictureMime maps allowed extensions to MIME types; anything else
// is rejected with 422.
var pictureMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
}

// WithImages wires the blob backend and the bundled-default provider.
// Without both, the picture surface mounts read-only or
// not at all (see APIRoutes).
func WithImages(images ImageStore, defaults DefaultPictureFunc) ServiceOption {
	return func(s *Service) { s.images, s.defaultPicture = images, defaults }
}

// setPictureCache mirrors the bundled-image cache policy (15 min
// fresh, 1 day stale-while-revalidate).
func setPictureCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=900, stale-while-revalidate=86400")
}

// serveProfilePicture serves GET /users/{id}/profile-picture.png —
// bare image bytes without an envelope, with cache headers
// included. Fallback chain: custom blob → bundled default → 404.
func (s *Service) serveProfilePicture(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	u, err := s.store.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	if u.ProfilePicturePath != nil && s.images != nil {
		if reader, size, mime, found := s.openPicture(r, *u.ProfilePicturePath); found {
			defer reader.Close()
			w.Header().Set("Content-Type", mime)
			setPictureCache(w)
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			_, _ = io.Copy(w, reader)
			return
		}
	}

	if s.defaultPicture != nil {
		if reader, size, mime, ok := s.defaultPicture(r.Context()); ok {
			defer reader.Close()
			w.Header().Set("Content-Type", mime)
			setPictureCache(w)
			w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
			_, _ = io.Copy(w, reader)
			return
		}
	}

	responder.NotFoundJSON(w, r)
}

// openPicture reads one blob, mapping storage misses to not-found.
func (s *Service) openPicture(r *http.Request, picturePath string) (io.ReadCloser, int64, string, bool) {
	reader, size, err := s.images.Open(r.Context(), picturePath)
	if err != nil {
		return nil, 0, "", false
	}
	ext := path.Ext(picturePath)
	mime := pictureMime[ext]
	if mime == "" {
		reader.Close()
		return nil, 0, "", false
	}
	return reader, size, mime, true
}
