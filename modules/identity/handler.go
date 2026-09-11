package identity

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

func (m *Module) apiRoot(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

type createUserRequest struct {
	Name string `json:"name"`
}

func (m *Module) createUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responder.BadRequestJSON(w, "invalid request body")
		return
	}

	user, err := m.service.Create(r.Context(), req.Name)
	if err != nil {
		responder.BadRequestJSON(w, err.Error())
		return
	}

	responder.WriteJSON(w, http.StatusCreated, user)
}

func (m *Module) listUsers(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, m.service.List(r.Context()))
}

func (m *Module) getUser(w http.ResponseWriter, r *http.Request) {
	user, err := m.service.GetByID(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	responder.WriteJSON(w, http.StatusOK, user)
}
