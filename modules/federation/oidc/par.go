package oidc

// par.go implements Pushed Authorization Requests (RFC 9126) use
// cases: a client pushes its authorize parameters to the server and
// receives a one-time request_uri; the browser flow then references
// it. The HTTP shell lives in handler_device.go alongside the other
// bare-protocol shells.

import (
	"context"
	"strings"
	"time"
)

// pushedAuthorizationResponse is the RFC 9126 §3.2 payload.
type pushedAuthorizationResponse struct {
	RequestURI string `json:"request_uri"`
	ExpiresIn  int    `json:"expires_in"`
}

// pushPAR validates the pushed parameters as far as the
// unredirected request allows and stores them as a one-time PAR
// session; the /authorize resume re-validates everything else.
func (s *Service) pushPAR(ctx context.Context, params *authorizeParams) (*pushedAuthorizationResponse, *tokenFailure) {
	client, err := s.store.GetClient(ctx, params.ClientID)
	if err != nil {
		return nil, invalidRequest()
	}
	if !client.MatchesCallback(params.RedirectURI) || params.ResponseType != "code" {
		return nil, invalidRequest()
	}
	if !scopeList(params.Scope)[ScopeOpenID] {
		return nil, &tokenFailure{Code: "invalid_scope", Status: 400}
	}
	if params.CodeChallenge == "" || params.CodeChallengeMethod != "S256" {
		return nil, invalidRequest()
	}

	requestURI, err := randomToken()
	if err != nil {
		return nil, serverError()
	}

	if err := s.store.PutSession(ctx, OAuth2Session{
		Kind:      KindPAR,
		Key:       sha256Hex(requestURI),
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
		return nil, serverError()
	}

	s.record(ctx, "oidc_par_created", map[string]any{
		"client_id": client.ID.String(),
	})
	return &pushedAuthorizationResponse{
		RequestURI: "urn:ietf:params:oauth:request_uri:" + requestURI,
		ExpiresIn:  int(PARTTL.Seconds()),
	}, nil
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
