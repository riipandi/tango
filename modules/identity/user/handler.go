package user

import (
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/pkg/responder"
)

// The one REST route the feature serves: the picture read. It exists beside
// the RPC surface because a picture is fetched by an <img> tag, not by a
// client that speaks the protocol — the URL travels in the SPA's markup and
// in emails, and neither follows a redirect through the RPC procedure set.
//
// The read is public: a picture the account has answers its bytes, and an
// account without one answers the bundled default by redirect — the relative
// location resolves against the host the client reached, so the same handler
// serves the dev server's proxy and the production binary. An unknown
// identifier is the one refusal, in the envelope the REST surface answers.
func (m *Module) Mount(r chi.Router) {
	r.Get("/api/users/{id}/profile-picture.png", func(w http.ResponseWriter, r *http.Request) {
		picture, err := m.service.ProfilePicture(r.Context(), chi.URLParam(r, "id"))
		switch {
		case errors.Is(err, ErrUserNotFound):
			responder.Fail(w, r, http.StatusNotFound, "user not found")
			return
		case errors.Is(err, ErrPicturesUnavailable):
			responder.Fail(w, r, http.StatusServiceUnavailable, "picture storage is not available")
			return
		case err != nil:
			responder.WriteError(w, r, err)
			return
		}
		defer picture.Body.Close()

		if picture.Default {
			http.Redirect(w, r, DefaultPicturePath, http.StatusFound)
			return
		}

		// The key names one account's picture and its content changes on an
		// update, so the cache holds briefly rather than forever.
		w.Header().Set("Content-Type", picture.ContentType)
		w.Header().Set("Cache-Control", "private, max-age=60")
		// A copy failure after the headers are written is a client that went
		// away mid-body; there is no answer left to send.
		if _, err := io.Copy(w, picture.Body); err != nil {
			responder.WriteError(w, r, err)
		}
	})
}
