package appimage

import (
	"io"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// maxUploadSize bounds one image upload.
const maxUploadSize = 5 << 20 // 5 MiB

// APIRoutes mounts the application-images endpoints.
func (s *Service) APIRoutes(r chi.Router) {
	r.Get("/application-images/logo", s.serveLogo)
	r.Get("/application-images/email", s.serveImage(ImageEmailLogo))
	r.Get("/application-images/background", s.serveImage(ImageBackground))
	r.Get("/application-images/favicon", s.serveImage(ImageFavicon))
	r.Get("/application-images/default-profile-picture", s.serveImage(ImageProfilePic))

	r.With(guard).Put("/application-images/logo", s.updateLogo)
	r.With(guard).Delete("/application-images/logo", s.deleteLogo)
	r.With(guard).Put("/application-images/email", s.updateImage(ImageEmailLogo))
	r.With(guard).Delete("/application-images/email", s.deleteImage(ImageEmailLogo))
	r.With(guard).Put("/application-images/background", s.updateImage(ImageBackground))
	r.With(guard).Delete("/application-images/background", s.deleteImage(ImageBackground))
	r.With(guard).Put("/application-images/favicon", s.updateImage(ImageFavicon))
	r.With(guard).Delete("/application-images/favicon", s.deleteImage(ImageFavicon))
	r.With(guard).Put("/application-images/default-profile-picture", s.updateImage(ImageProfilePic))
	r.With(guard).Delete("/application-images/default-profile-picture", s.deleteImage(ImageProfilePic))
}

var _ identity.APIFeature = &Service{}

// Name implements identity.Feature.
func (*Service) Name() string { return "appimage" }

// guard is replaced at wiring time via WithGuard; unguarded builds deny mutations (fail closed).
var guard = func(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
	})
}

// WithGuard sets the admin middleware protecting mutations.
func (s *Service) WithGuard(g func(http.Handler) http.Handler) *Service {
	guard = g
	return s
}

func (s *Service) serveLogo(w http.ResponseWriter, r *http.Request) {
	name := ImageLogoDark
	if r.URL.Query().Get("light") == "true" {
		name = ImageLogoLight
	}
	s.serve(w, r, name)
}

func (s *Service) serveImage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.serve(w, r, name)
	}
}

func (s *Service) serve(w http.ResponseWriter, r *http.Request, name string) {
	reader, size, mime, err := s.GetImage(r.Context(), name)
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "image_not_found")
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", CacheControl(r.URL.Query().Get("skipCache") != ""))
	w.Header().Set("Content-Length", strconv.FormatInt(size, 10))
	_, _ = io.Copy(w, reader)
}

func (s *Service) updateLogo(w http.ResponseWriter, r *http.Request) {
	name := ImageLogoDark
	if r.URL.Query().Get("light") == "true" {
		name = ImageLogoLight
	}
	s.update(w, r, name)
}

func (s *Service) updateImage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.update(w, r, name)
	}
}

func (s *Service) update(w http.ResponseWriter, r *http.Request, name string) {
	file, header, err := r.FormFile("file")
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "validation failed",
			responder.WithError("multipart field 'file' is required"))
		return
	}
	defer file.Close()
	if header.Size > maxUploadSize {
		responder.Fail(w, r, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	if err := s.UpdateImage(r.Context(), name, header.Filename, file); err != nil {
		respondServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) deleteLogo(w http.ResponseWriter, r *http.Request) {
	name := ImageLogoDark
	if r.URL.Query().Get("light") == "true" {
		name = ImageLogoLight
	}
	s.delete(w, r, name)
}

func (s *Service) deleteImage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.delete(w, r, name)
	}
}

func (s *Service) delete(w http.ResponseWriter, r *http.Request, name string) {
	if err := s.DeleteImage(r.Context(), name); err != nil {
		respondServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func respondServiceError(w http.ResponseWriter, r *http.Request, err error) {
	switch err {
	case ErrNotFound, ErrInvalidName:
		responder.Fail(w, r, http.StatusNotFound, "image_not_found")
	case ErrUnsupported:
		responder.Fail(w, r, http.StatusUnprocessableEntity, "unsupported_file_type")
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
