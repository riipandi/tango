package signup

// handler.go owns the signup HTTP surface: anonymous signup/setup
// (policy-gated) plus the admin signup-token CRUD.

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// duration aliases time.Duration for TTL fields.
type duration = time.Duration

// Feature is the wireable signup unit.
type Feature struct {
	service    *Service
	cookieName string
}

// New wires the feature to its service.
func New(service *Service) Feature { return Feature{service: service} }

// Name names the feature for logs.
func (Feature) Name() string { return "signup" }

// WithCookie wires the session cookie settings (signup issues a
// session).
func (f Feature) WithCookie(name string, secure bool) Feature {
	f.cookieName = name
	return f
}

// APIRoutes mounts the signup endpoints relative to the /api group.
func (f Feature) APIRoutes(r chi.Router, g identity.RouteGroups) {
	r.Get("/signup/setup", f.service.handleSetupAvailable)
	r.Post("/signup", f.service.handleSignUp)
	r.Post("/signup/setup", f.service.handleSetup)

	if g.Admin == nil {
		return
	}
	admin := r.With(g.Admin)
	admin.Get("/signup-tokens", f.service.handleListTokens)
	admin.Post("/signup-tokens", f.service.handleCreateToken)
	admin.Delete("/signup-tokens/{id}", f.service.handleDeleteToken)
}

// signUpRequest is the POST /signup (+/setup) payload.
type signUpRequest struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	FirstName string `json:"first_name,omitzero"`
	LastName  string `json:"last_name,omitzero"`
	Token     string `json:"token,omitzero"`
}

func (r signUpRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Username, validation.Required),
		validation.Field(&r.Email, validation.Required),
	)
}

// createTokenRequest is the POST /signup-tokens payload.
type createTokenRequest struct {
	TTL        string   `json:"ttl"`
	UsageLimit int      `json:"usage_limit"`
	GroupIDs   []string `json:"user_group_ids"`
}

func (r createTokenRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.UsageLimit, validation.Required, validation.Min(1)),
	)
}

// handleSignUp serves POST /signup: token-gated account creation.
func (s *Service) handleSignUp(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	result, err := s.SignUp(r.Context(), SignUpRequest(req), false)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, result.User)
}

// handleSetupAvailable serves GET /signup/setup: 204 while the
// initial-admin setup can still run, 404 once any user exists.
func (s *Service) handleSetupAvailable(w http.ResponseWriter, r *http.Request) {
	available, err := s.SetupAvailable(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	if !available {
		responder.Fail(w, r, http.StatusNotFound, "setup not available")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleSetup serves POST /signup/setup: first-admin bootstrap —
// rejected with 409 once any user exists.
func (s *Service) handleSetup(w http.ResponseWriter, r *http.Request) {
	var req signUpRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	result, err := s.SignUp(r.Context(), SignUpRequest(req), true)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, result.User)
}

// handleListTokens serves GET /signup-tokens (admin).
func (s *Service) handleListTokens(w http.ResponseWriter, r *http.Request) {
	tokens, err := s.ListTokens(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	views := make([]map[string]any, 0, len(tokens))
	for _, token := range tokens {
		views = append(views, tokenView(token))
	}
	responder.Success(w, r, http.StatusOK, views)
}

// handleCreateToken serves POST /signup-tokens (admin): raw shown
// once.
func (s *Service) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req createTokenRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	ttl, parseErr := parseTTL(req.TTL)
	if parseErr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "ttl must be a duration (e.g. 24h)")
		return
	}

	token, raw, err := s.CreateToken(r.Context(), CreateParams{
		TTL:        ttl,
		UsageLimit: req.UsageLimit,
		GroupIDs:   req.GroupIDs,
	})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to create token")
		return
	}

	view := tokenView(*token)
	view["token"] = raw
	responder.Success(w, r, http.StatusCreated, view)
}

// handleDeleteToken serves DELETE /signup-tokens/{id} (admin).
func (s *Service) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[SignupTokenID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	if err := s.DeleteToken(r.Context(), id); err != nil {
		if err == ErrNotFound {
			responder.NotFoundJSON(w, r)
			return
		}
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tokenView is the API payload; the hash never leaves the store.
func tokenView(t SignupToken) map[string]any {
	return map[string]any{
		"id":          t.ID.String(),
		"usage_limit": t.UsageLimit,
		"usage_count": t.UsageCount,
		"created_at":  t.CreatedAt,
		"expires_at":  t.ExpiresAt,
		"user_groups": t.GroupIDs,
	}
}

// parseTTL parses a Go duration string.
func parseTTL(raw string) (ttl duration, err error) {
	if raw == "" {
		return 0, nil
	}
	parsed, parseErr := time.ParseDuration(raw)
	if parseErr != nil {
		return 0, parseErr
	}
	return duration(parsed), nil
}

// writeError maps signup failures to statuses.
func (s *Service) writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch err {
	case ErrNotFound:
		responder.Fail(w, r, http.StatusNotFound, err.Error())
	case ErrSetupCompleted:
		responder.Fail(w, r, http.StatusConflict, err.Error())
	case ErrExhausted, ErrInvalidIDs:
		responder.Fail(w, r, http.StatusUnprocessableEntity, err.Error())
	default:
		if errors.Is(err, user.ErrInvalidUsername) || errors.Is(err, user.ErrInvalidEmail) {
			responder.Fail(w, r, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if errors.Is(err, user.ErrDuplicate) {
			responder.Fail(w, r, http.StatusConflict, err.Error())
			return
		}
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
