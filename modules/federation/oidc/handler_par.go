package oidc

// handler_par.go is the HTTP surface of the PAR endpoint: bare
// OAuth errors, client-authenticated, form-encoded.

import (
	"net/http"

	"github.com/riipandi/tango/pkg/responder"
)

// HandlePAR implements POST /api/oidc/par (client-authenticated,
// form-encoded).
func (s *Service) HandlePAR(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	params, err := parseAuthorizeForm(r.PostForm)
	if err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	response, failure := s.pushPAR(r.Context(), params)
	if failure != nil {
		writeTokenFailure(w, r, failure)
		return
	}
	responder.WriteJSON(w, http.StatusOK, response)
}
