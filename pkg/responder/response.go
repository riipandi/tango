package responder

import (
	"encoding/json"
	"net/http"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	}
}

func NotFoundJSON(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusNotFound, map[string]string{
		"error": "not found",
		"path":  r.URL.Path,
	})
}

func MethodNotAllowedJSON(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusMethodNotAllowed, map[string]string{
		"error":  "method not allowed",
		"method": r.Method,
	})
}

func BadRequestJSON(w http.ResponseWriter, msg string) {
	WriteJSON(w, http.StatusBadRequest, map[string]string{
		"error": msg,
	})
}
