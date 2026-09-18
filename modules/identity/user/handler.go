package user

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

// writeError maps user domain errors onto HTTP statuses; anything
// else keeps the shared mapping. Used by the picture surface too.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		responder.NotFoundJSON(w, r)
	case errors.Is(err, ErrDuplicate):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrInvalidEmail):
		responder.Fail(w, r, http.StatusBadRequest, err.Error())
	default:
		responder.WriteError(w, r, err)
	}
}

// APIRoutes mounts the retained user endpoints inside the shared
// /api group: the bare .png profile-picture read. All other user
// surfaces serve ConnectRPC below /rpc; email flows stay on their
// feature routes.
func (s *Service) APIRoutes(r chi.Router, _ identity.RouteGroups) {
	r.Get("/users/{id}/profile-picture.png", s.serveProfilePicture)
}
