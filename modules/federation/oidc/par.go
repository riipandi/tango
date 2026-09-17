package oidc

// par.go implements Pushed Authorization Requests (RFC 9126): a
// client pushes its authorize parameters to the server and receives
// a one-time request_uri; the browser flow then references it.

import (
	"net/http"
	"strings"
	"time"

	"github.com/riipandi/tango/pkg/responder"
)

// pushedAuthorizationResponse is the RFC 9126 §3.2 payload.
type pushedAuthorizationResponse struct {
	RequestURI string `json:"request_uri"`
	ExpiresIn  int    `json:"expires_in"`
}

// HandlePAR implements POST /api/oidc/par (client-authenticated,
// form-encoded). The pushed parameters are validated as far as the
// unredirected request allows; the /authorize resume re-validates
// everything else. Bare OAuth errors: this is a protocol endpoint.
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

	client, err := s.store.GetClient(r.Context(), params.ClientID)
	if err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}
	if !client.MatchesCallback(params.RedirectURI) || params.ResponseType != "code" {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}
	if !scopeList(params.Scope)[ScopeOpenID] {
		tokenError(w, r, "invalid_scope", http.StatusBadRequest)
		return
	}
	if params.CodeChallenge == "" || params.CodeChallengeMethod != "S256" {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	requestURI, err := randomToken()
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	sum := sha256Hex(requestURI)
	if err := s.store.PutSession(r.Context(), OAuth2Session{
		Kind:      KindPAR,
		Key:       sum,
		RequestID: NewID().String(),
		Active:    true,
		RequestData: map[string]any{
			"client_id":          client.ID.String(),
			"redirect_uri":       params.RedirectURI,
			"scope":              params.Scope,
			"state":              params.State,
			"nonce":              params.Nonce,
			"code_challenge":     params.CodeChallenge,
			"code_challenge_met": params.CodeChallengeMethod,
			"resource":           params.Resource,
			"prompt":             params.Prompt,
		},
		ClientID:  client.ID.String(),
		ExpiresAt: timeOfPtr(time.Now().UTC().Add(PARTTL)),
	}); err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	s.record(r.Context(), "oidc_par_created", map[string]any{
		"client_id": client.ID.String(),
	})
	responder.WriteJSON(w, http.StatusOK, pushedAuthorizationResponse{
		RequestURI: "urn:ietf:params:oauth:request_uri:" + requestURI,
		ExpiresIn:  int(PARTTL.Seconds()),
	})
}

// parRequestURI extracts a pushed request_uri value; empty when the
// parameter is absent or malformed.
func parRequestURI(raw string) string {
	payload, ok := strings.CutPrefix(raw, "urn:ietf:params:oauth:request_uri:")
	if !ok || payload == "" {
		return ""
	}
	return payload
}

func timeOfPtr(t time.Time) *time.Time { return &t }
