package user

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responder.BadRequestJSON(w, "invalid request body")
		return
	}

	user, err := s.Create(r.Context(), req.Name)
	if err != nil {
		responder.BadRequestJSON(w, err.Error())
		return
	}

	responder.WriteJSON(w, http.StatusCreated, user)
}

func (s *Service) listUsers(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, s.List(r.Context()))
}

func (s *Service) getUser(w http.ResponseWriter, r *http.Request) {
	user, err := s.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	responder.WriteJSON(w, http.StatusOK, user)
}
