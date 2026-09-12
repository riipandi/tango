package user

import (
	"net/http"

	jsonv2 "encoding/json/v2"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
)

type createUserRequest struct {
	Name string `json:"name"`
}

// APIRoutes mounts the user endpoints inside the shared /api group.
func (s *Service) APIRoutes(r chi.Router) {
	r.Post("/users", s.createUser)
	r.Get("/users", s.listUsers)
	r.Get("/users/{id}", s.getUser)
}

func (s *Service) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := jsonv2.UnmarshalRead(r.Body, &req); err != nil {
		responder.BadRequestJSON(w, r, "invalid request body")
		return
	}

	user, err := s.Create(r.Context(), req.Name)
	if err != nil {
		responder.BadRequestJSON(w, r, err.Error())
		return
	}

	responder.Success(w, r, http.StatusCreated, user)
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) {
	responder.Success(w, r, http.StatusOK, s.List(r.Context()))
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[identity.UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	user, err := s.GetByID(r.Context(), id)
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	responder.Success(w, r, http.StatusOK, user)
}
