package webauthn

// handler.go owns the passkey HTTP surface: ceremonies (self for
// registration, anonymous for login) plus the admin credential
// CRUD. Begin endpoints return the raw WebAuthn options JSON; finish
// endpoints take the browser's JSON assertion body.

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
	"github.com/riipandi/tango/pkg/validate"
)

// Feature is the wireable webauthn unit.
type Feature struct {
	service *Service
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "webauthn" }

// APIRoutes mounts the passkey endpoints relative to the /api group.
// Without wired groups the affected routes are simply skipped —
// fail closed.
func (f Feature) APIRoutes(r chi.Router, g identity.RouteGroups) {
	// Anonymous: discoverable login ceremony.
	r.Post("/webauthn/login/begin", f.service.handleBeginLogin)
	r.Post("/webauthn/login/finish", f.service.handleFinishLogin)

	if g.Self != nil {
		self := r.With(g.Self)
		self.Post("/webauthn/register/begin", f.service.handleBeginRegistration)
		self.Post("/webauthn/register/finish", f.service.handleFinishRegistration)
	}

	if g.Admin != nil {
		admin := r.With(g.Admin)
		admin.Get("/users/{id}/webauthn-credentials", f.service.handleListCredentials)
		admin.Delete("/users/{id}/webauthn-credentials/{credentialId}", f.service.handleDeleteCredential)
		admin.Put("/users/{id}/webauthn-credentials/{credentialId}", f.service.handleRenameCredential)
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
	// The browser-facing shape is upstream's bare {publicKey}
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

	setSessionCookie(w, s.cookieName, token, s.cookieSecure)
	responder.Success(w, r, http.StatusOK, u)
}

// renameCredentialRequest is the PUT credential payload.
type renameCredentialRequest struct {
	Name string `json:"name"`
}

// handleListCredentials serves GET /users/{id}/webauthn-credentials.
func (s *Service) handleListCredentials(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}

	credentials, err := s.ListCredentials(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]map[string]any, 0, len(credentials))
	for _, credential := range credentials {
		views = append(views, credentialView(credential))
	}
	responder.Success(w, r, http.StatusOK, views)
}

// handleDeleteCredential serves DELETE
// /users/{id}/webauthn-credentials/{credentialId}.
func (s *Service) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}
	credentialID, ok := parseCredentialID(r)
	if !ok {
		return
	}

	if err := s.DeleteCredential(r.Context(), userID, credentialID); err != nil {
		if errors.Is(err, ErrNotFound) {
			responder.NotFoundJSON(w, r)
			return
		}
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRenameCredential serves PUT
// /users/{id}/webauthn-credentials/{credentialId} — body {name}.
func (s *Service) handleRenameCredential(w http.ResponseWriter, r *http.Request) {
	userID, ok := parseUserIDParam(r)
	if !ok {
		responder.NotFoundJSON(w, r)
		return
	}
	credentialID, ok := parseCredentialID(r)
	if !ok {
		return
	}

	var req renameCredentialRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	credential, err := s.RenameCredential(r.Context(), userID, credentialID, req.Name)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			responder.NotFoundJSON(w, r)
			return
		}
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, credentialView(*credential))
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

// parseUserIDParam resolves the {id} path segment.
func parseUserIDParam(r *http.Request) (user.UserID, bool) {
	id, err := identity.ParseID[user.UserID](chi.URLParam(r, "id"))
	return id, err == nil
}

// parseCredentialID resolves the {credentialId} path segment.
func parseCredentialID(r *http.Request) (CredentialID, bool) {
	id, err := identity.ParseID[CredentialID](chi.URLParam(r, "credentialId"))
	return id, err == nil
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

// setSessionCookie mirrors the session module cookie (name + flags);
// the duplicate is intentional — webauthn must not import the
// session package (the registry wires the cookie name).
func setSessionCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- session cookie parity (SameSite=Lax, Secure off in dev)
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   0,
		HttpOnly: true,
		Secure:   secure,
	})
}
