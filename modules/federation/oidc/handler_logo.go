package oidc

// handler_logo.go serves the phase 9C client-logo surface: a public
// bare-bytes read plus admin upload/delete. Upstream transcodes
// uploads; tango stores the file as-is behind a MIME allowlist (same
// deviation as the user profile picture). Dark-variant logos stay a
// 9C+ follow-up.

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strconv"

	"github.com/riipandi/tango/pkg/responder"
)

// maxLogoUpload bounds one client-logo upload.
const maxLogoUpload = 5 << 20 // 5 MiB, matches the appimage cap

// logoMime maps allowed logo extensions to MIME types.
var logoMime = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".svg":  "image/svg+xml",
	".webp": "image/webp",
}

// ClientImageStore is the consumer-side blob adapter; registry wires
// the shared storage backend here.
type ClientImageStore interface {
	Save(ctx context.Context, path string, data io.Reader) error
	Open(ctx context.Context, path string) (io.ReadCloser, int64, error)
	Delete(ctx context.Context, path string) error
}

// setLogoCache mirrors the appimage cache policy.
func setLogoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=900, stale-while-revalidate=86400")
}

// serveClientLogo serves GET /oidc/clients/{id}/logo — bare image
// bytes (no envelope), public like upstream.
func (s *Service) serveClientLogo(w http.ResponseWriter, r *http.Request) {
	client, ok := s.clientForLogo(w, r)
	if !ok {
		return
	}
	if client.LogoPath == nil {
		responder.NotFoundJSON(w, r)
		return
	}

	ext := path.Ext(*client.LogoPath)
	mime := logoMime[ext]
	if mime == "" {
		responder.NotFoundJSON(w, r)
		return
	}

	reader, size, err := s.images.Open(r.Context(), *client.LogoPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			responder.NotFoundJSON(w, r)
			return
		}
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", mime)
	setLogoCache(w)
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, reader)
}

// updateClientLogo handles POST /oidc/clients/{id}/logo: multipart
// 'file' → blob store → path column (+ image_type for meta).
func (s *Service) updateClientLogo(w http.ResponseWriter, r *http.Request) {
	client, ok := s.clientForLogo(w, r)
	if !ok {
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "validation failed",
			responder.WithError("multipart field 'file' is required"))
		return
	}
	defer file.Close()
	if header.Size > maxLogoUpload {
		responder.Fail(w, r, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	ext := path.Ext(header.Filename)
	if logoMime[ext] == "" {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "unsupported_file_type")
		return
	}

	logoPath := "client-logos/" + client.ID.String() + ext
	if err := s.images.Save(r.Context(), logoPath, file); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if err := s.store.SetClientLogoPath(r.Context(), client.ID, &logoPath); err != nil {
		_ = s.images.Delete(r.Context(), logoPath)
		writeClientError(w, r, err)
		return
	}

	// The replaced blob may carry a different extension — remove it.
	if client.LogoPath != nil && *client.LogoPath != logoPath {
		_ = s.images.Delete(r.Context(), *client.LogoPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteClientLogo handles DELETE: clear columns, remove the blob
// (a missing blob stays a success).
func (s *Service) deleteClientLogo(w http.ResponseWriter, r *http.Request) {
	client, ok := s.clientForLogo(w, r)
	if !ok {
		return
	}

	if err := s.store.SetClientLogoPath(r.Context(), client.ID, nil); err != nil {
		writeClientError(w, r, err)
		return
	}
	if client.LogoPath != nil {
		_ = s.images.Delete(r.Context(), *client.LogoPath)
	}
	w.WriteHeader(http.StatusNoContent)
}

// clientForLogo resolves + validates the {clientId} URL param.
func (s *Service) clientForLogo(w http.ResponseWriter, r *http.Request) (Client, bool) {
	id, ok := clientIDParam(w, r)
	if !ok {
		return Client{}, false
	}
	client, err := s.store.GetClient(r.Context(), id)
	if err != nil {
		writeClientError(w, r, err)
		return Client{}, false
	}
	return client, true
}
