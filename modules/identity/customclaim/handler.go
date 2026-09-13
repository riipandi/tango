package customclaim

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/modules/identity/usergroup"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// createClaimRequest is the POST payload for both owner scopes.
type createClaimRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (r createClaimRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Key, validation.Required, validation.Length(1, 100)),
		validation.Field(&r.Value, validation.Required),
	)
}

// updateClaimRequest is the PUT payload (value only — key identifies).
type updateClaimRequest struct {
	Value string `json:"value"`
}

func (r updateClaimRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Value, validation.Required),
	)
}

// APIRoutes mounts the claim endpoints inside the shared /api group,
// behind the admin guard when one is wired.
func (s *Service) APIRoutes(r chi.Router) {
	mount := func(ar chi.Router) {
		ar.Get("/custom-claims/suggestions", s.suggestions)
		ar.Get("/custom-claims/user/{userId}", s.listForUser)
		ar.Post("/custom-claims/user/{userId}", s.createForUser)
		ar.Put("/custom-claims/user/{userId}/{claimId}", s.updateForUser)
		ar.Delete("/custom-claims/user/{userId}/{claimId}", s.deleteForUser)
		ar.Get("/custom-claims/user-group/{userGroupId}", s.listForGroup)
		ar.Post("/custom-claims/user-group/{userGroupId}", s.createForGroup)
		ar.Put("/custom-claims/user-group/{userGroupId}/{claimId}", s.updateForGroup)
		ar.Delete("/custom-claims/user-group/{userGroupId}/{claimId}", s.deleteForGroup)
	}

	if s.guard == nil {
		mount(r)
		return
	}
	r.Group(func(ar chi.Router) {
		ar.Use(s.guard)
		mount(ar)
	})
}

func (s *Service) suggestions(w http.ResponseWriter, r *http.Request) {
	keys, err := s.SuggestedKeys(r.Context())
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, keys)
}

func (s *Service) listForUser(w http.ResponseWriter, r *http.Request) {
	userID, err := identity.ParseID[user.UserID](chi.URLParam(r, "userId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	claims, err := s.ListByUser(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, claims)
}

func (s *Service) createForUser(w http.ResponseWriter, r *http.Request) {
	userID, err := identity.ParseID[user.UserID](chi.URLParam(r, "userId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req createClaimRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		writeValidation(w, r, verr)
		return
	}

	claim, err := s.CreateForUser(r.Context(), userID, UpsertParams{Key: req.Key, Value: req.Value})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, claim)
}

func (s *Service) updateForUser(w http.ResponseWriter, r *http.Request) {
	claimID, err := identity.ParseID[CustomClaimID](chi.URLParam(r, "claimId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req updateClaimRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		writeValidation(w, r, verr)
		return
	}

	claim, err := s.UpdateValue(r.Context(), claimID, req.Value)
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, claim)
}

func (s *Service) deleteForUser(w http.ResponseWriter, r *http.Request) {
	claimID, err := identity.ParseID[CustomClaimID](chi.URLParam(r, "claimId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	if err := s.Delete(r.Context(), claimID); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Service) listForGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := identity.ParseID[usergroup.UserGroupID](chi.URLParam(r, "userGroupId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	claims, err := s.ListByGroup(r.Context(), groupID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, claims)
}

func (s *Service) createForGroup(w http.ResponseWriter, r *http.Request) {
	groupID, err := identity.ParseID[usergroup.UserGroupID](chi.URLParam(r, "userGroupId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req createClaimRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		writeValidation(w, r, verr)
		return
	}

	claim, err := s.CreateForGroup(r.Context(), groupID, UpsertParams{Key: req.Key, Value: req.Value})
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, claim)
}

func (s *Service) updateForGroup(w http.ResponseWriter, r *http.Request) {
	claimID, err := identity.ParseID[CustomClaimID](chi.URLParam(r, "claimId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req updateClaimRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		writeValidation(w, r, verr)
		return
	}

	claim, err := s.UpdateValue(r.Context(), claimID, req.Value)
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, claim)
}

func (s *Service) deleteForGroup(w http.ResponseWriter, r *http.Request) {
	claimID, err := identity.ParseID[CustomClaimID](chi.URLParam(r, "claimId"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	if err := s.Delete(r.Context(), claimID); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

func writeValidation(w http.ResponseWriter, r *http.Request, err error) {
	responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
		responder.WithError(validate.FieldErrors(err)))
}

// writeError maps module errors to the response envelope.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		responder.NotFoundJSON(w, r)
	case errors.Is(err, ErrDuplicate):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
