package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/transport/responder"
)

type createUserRequest struct {
	Name string `json:"name"`
}

var (
	users = []map[string]string{
		{"id": "1", "name": "User One"},
		{"id": "2", "name": "User Two"},
	}
	nextID  int = 3
	usersMu sync.Mutex
)

func ListUsersHandler(w http.ResponseWriter, r *http.Request) {
	usersMu.Lock()
	defer usersMu.Unlock()

	responder.WriteJSON(w, http.StatusOK, users)
}

func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		responder.BadRequestJSON(w, "invalid request body")
		return
	}

	if req.Name == "" {
		responder.BadRequestJSON(w, "name is required")
		return
	}

	usersMu.Lock()
	id := nextID
	nextID++
	user := map[string]string{"id": fmt.Sprintf("%d", id), "name": req.Name}
	users = append(users, user)
	usersMu.Unlock()

	responder.WriteJSON(w, http.StatusCreated, user)
}

func GetUserHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"id": id,
	})
}
