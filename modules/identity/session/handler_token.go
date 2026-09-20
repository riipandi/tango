package session

import (
	"net/http"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// tokenBridge is POST /api/auth/token — the refresh channel. The
// client posts the session token it holds; the answer carries a fresh
// bearer access token, and the session token itself when it rotated
// (the old one stops resolving). Nothing is stored server-side on
// behalf of the client, so the endpoint is stateless.
func (s *Service) tokenBridge(w http.ResponseWriter, r *http.Request) {
	if s.tokens == nil {
		responder.Fail(w, r, http.StatusNotImplemented, "token bridge is not configured")
		return
	}

	var body tokenBridgeRequest
	if verr := validate.Request(r.Body, &body); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	refresh := body.SessionToken
	u, se, err := s.Resolve(r.Context(), refresh)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "invalid or expired token")
		return
	}

	principal := kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}
	newRefresh, _, err := s.Rotate(r.Context(), se)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	access, expiresAt, err := s.IssueAccess(r.Context(), principal)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, r, http.StatusOK, tokenBridgeResponse{
		AccessToken:  access,
		TokenType:    "Bearer",
		ExpiresIn:    int(time.Until(expiresAt).Seconds()),
		ExpiresAt:    expiresAt,
		SessionToken: newRefresh,
	})
}

// tokenBridgeRequest is the refresh payload: the session token the
// client holds.
type tokenBridgeRequest struct {
	SessionToken string `json:"session_token"`
}

// Validate checks the refresh payload.
func (r tokenBridgeRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.SessionToken, validation.Required),
	)
}

// tokenBridgeResponse is the client-facing refresh payload. The
// session token is always present: the presented one is dead after
// the call, so the client must replace it.
type tokenBridgeResponse struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	ExpiresIn    int       `json:"expires_in"`
	ExpiresAt    time.Time `json:"expires_at"`
	SessionToken string    `json:"session_token"`
}
