package oidc

// handler_logo.go serves public reads and admin writes for client logos.

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
const maxLogoUpload = 5 << 20 // 5 MiB, matches the profile-picture cap

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

// setLogoCache mirrors the image cache policy.
func setLogoCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "public, max-age=900, stale-while-revalidate=86400")
}

// serveClientLogo serves GET /oidc/clients/{id}/logo — bare image
// bytes without an API envelope.
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
