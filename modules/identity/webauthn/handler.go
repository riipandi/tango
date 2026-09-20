package webauthn

// handler.go owns the passkey HTTP surface: ceremonies (self for
// registration, anonymous for login). Admin credential management
// serves ConnectRPC through the user RPC port. Begin endpoints
// return the raw WebAuthn options JSON; finish endpoints take the
// browser's JSON assertion body.

import (
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-webauthn/webauthn/protocol"

	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
)

// Feature is the wireable webauthn unit.
type Feature struct {
	service *Service
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "webauthn" }

// APIRoutes mounts the passkey ceremony endpoints relative to the
// /api group (browser WebAuthn contract). The admin credential CRUD
// serves ConnectRPC exclusively — see the user RPC port.
func (f Feature) APIRoutes(r chi.Router, g identity.RouteGroups) {
	// Anonymous: discoverable login ceremony.
	r.Post("/webauthn/login/begin", f.service.handleBeginLogin)
	r.Post("/webauthn/login/finish", f.service.handleFinishLogin)

	if g.Self != nil {
		self := r.With(g.Self)
		self.Post("/webauthn/register/begin", f.service.handleBeginRegistration)
		self.Post("/webauthn/register/finish", f.service.handleFinishRegistration)
	}
}

// handleBeginRegistration serves POST /webauthn/register/begin:
// returns the creation options + ceremony session id.
func (s *Service) handleBeginRegistration(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	options, sessionID, err := s.BeginRegistration(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to start passkey registration")
		return
	}
	// The browser-facing shape is the bare {publicKey} object
	// document; the ceremony id rides in the body because tango
	// tracks ceremonies statelessly instead of by cookie.
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"publicKey":  options,
		"session_id": sessionID,
	})
}

// handleFinishRegistration serves POST /webauthn/register/finish:
// consumes the ceremony (one-time) and stores the passkey.
func (s *Service) handleFinishRegistration(w http.ResponseWriter, r *http.Request) {
	principal, ok := middleware.PrincipalFromContext(r.Context())
	if !ok {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}
	sessionID := r.URL.Query().Get("session_id")
	if sessionID == "" {
		responder.Fail(w, r, http.StatusBadRequest, "session_id is required")
		return
	}
	userID, err := identity.ParseID[user.UserID](principal.UserID)
	if err != nil {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	// The service prepares (user + one-time ceremony session); the
	// protocol ceremony itself runs here against the browser request.
	waUser, session, err := s.PrepareRegistration(r.Context(), sessionID, userID)
	if err != nil {
		s.writeCeremonyError(w, r, err)
		return
	}
	credential, err := s.webAuthn.FinishRegistration(waUser, session, r)
	if err != nil {
		s.writeCeremonyError(w, r, classifyAssertionError(err))
		return
	}
	stored, err := s.StoreRegistration(r.Context(), userID, credential)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusCreated, credentialView(stored))
}

// handleBeginLogin serves POST /webauthn/login/begin (anonymous):
// discoverable-request options.
func (s *Service) handleBeginLogin(w http.ResponseWriter, r *http.Request) {
	options, sessionID, err := s.BeginLogin(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to start passkey login")
		return
	}
	responder.WriteJSON(w, http.StatusOK, map[string]any{
		"publicKey":  options,
		"session_id": sessionID,
	})
}

// handleFinishLogin serves POST /webauthn/login/finish: verifies
// the assertion and sets the sign-in session cookie.
func (s *Service) handleFinishLogin(w http.ResponseWriter, r *http.Request) {
	parsed, err := protocol.ParseCredentialRequestResponseBody(r.Body)
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid assertion body")
		return
	}

	u, token, err := s.VerifyLogin(r.Context(), r.URL.Query().Get("session_id"), parsed)
	if err != nil {
		s.writeCeremonyError(w, r, err)
		return
	}

	responder.Success(w, r, http.StatusOK, map[string]any{
		"user":          u,
		"session_token": token,
	})
}

// writeCeremonyError maps ceremony failures to statuses.
func (s *Service) writeCeremonyError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrInvalidSession):
		responder.Fail(w, r, http.StatusBadRequest, "passkey session is invalid or expired")
	case errors.Is(err, ErrVerificationRequired):
		responder.Fail(w, r, http.StatusBadRequest, "user verification is required")
	case errors.Is(err, ErrAssertionFailed):
		responder.Fail(w, r, http.StatusUnauthorized, "passkey ceremony failed")
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}

// base64URL encodes bytes for the wire format.
func base64URL(raw []byte) string {
	return base64.RawURLEncoding.EncodeToString(raw)
}

// credentialView is the API payload.
func credentialView(c StoredCredential) map[string]any {
	return map[string]any{
		"id":               c.ID.String(),
		"name":             c.Name,
		"credential_id":    base64URL(c.CredentialID),
		"attestation_type": c.AttestationType,
		"transport":        c.Transport,
		"backup_eligible":  c.BackupEligible,
		"backup_state":     c.BackupState,
		"created_at":       c.CreatedAt,
		"last_used_at":     c.LastUsedAt,
	}
}
