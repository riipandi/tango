package usergroup

import (
	"errors"
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// writeError maps user-group domain errors onto HTTP statuses;
// anything else keeps the shared mapping.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		responder.NotFoundJSON(w, r)
	case errors.Is(err, ErrDuplicate):
		responder.Fail(w, r, http.StatusConflict, err.Error())
	default:
		responder.WriteError(w, r, err)
	}
}

// createGroupRequest is the POST /user-groups payload.
type createGroupRequest struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
}

func (r createGroupRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Required, validation.Match(user.UsernamePattern)),
		validation.Field(&r.DisplayName, validation.Required),
	)
}

// updateGroupRequest is the PUT /user-groups/{id} payload.
type updateGroupRequest struct {
	Name        *string `json:"name,omitzero"`
	DisplayName *string `json:"display_name,omitzero"`
}

func (r updateGroupRequest) Validate() error {
	return validation.ValidateStruct(&r,
		validation.Field(&r.Name, validation.Match(user.UsernamePattern)),
		validation.Field(&r.DisplayName, validation.NilOrNotEmpty),
	)
}

// setMembersRequest is the PUT /user-groups/{id}/users payload: the
// complete member list (POST semantics).
type setMembersRequest struct {
	UserIDs []string `json:"user_ids"`
}

func (r setMembersRequest) Validate() error {
	return nil
}

// setUserGroupsRequest is the PUT /users/{id}/user-groups payload:
// the complete group list for one user.
type setUserGroupsRequest struct {
	GroupIDs []string `json:"user_group_ids"`
}

func (r setUserGroupsRequest) Validate() error {
	return nil
}

// setAllowedClientsRequest is the PUT /user-groups/{id}/allowed-oidc-clients
// payload: the complete client allowlist for one group.
type setAllowedClientsRequest struct {
	ClientIDs []string `json:"oidc_client_ids"`
}

func (r setAllowedClientsRequest) Validate() error {
	return nil
}

// groupsResponse carries the group plus its member user IDs.
type groupsResponse struct {
	UserGroup
	MemberIDs []string `json:"member_ids"`
}

// APIRoutes mounts the group endpoints inside the shared /api group,
// behind the admin guard when one is wired.
func (s *Service) APIRoutes(r chi.Router) {
	mount := func(ar chi.Router) {
		ar.Get("/user-groups", s.list)
		ar.Post("/user-groups", s.create)
		ar.Get("/user-groups/{id}", s.get)
		ar.Put("/user-groups/{id}", s.update)
		ar.Delete("/user-groups/{id}", s.deleteGroup)
		ar.Get("/user-groups/{id}/users", s.memberIDs)
		ar.Put("/user-groups/{id}/users", s.setMembers)
		ar.Put("/user-groups/{id}/allowed-oidc-clients", s.setAllowedClients)
		ar.Get("/users/{id}/groups", s.groupsForUser)
		ar.Put("/users/{id}/user-groups", s.replaceUserGroups)
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

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var req createGroupRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	g, err := s.Create(r.Context(), CreateParams(req))
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, g)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	groups, total, err := s.List(r.Context(), ListParams{
		Query:            r.URL.Query().Get("query"),
		PaginationParams: params,
	})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, groups, responder.WithPaginationFrom(params, total))
}

// get resolves one group with member IDs.
func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	g, err := s.GetByID(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}

	members, err := s.MemberIDs(r.Context(), id)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	responder.Success(w, r, http.StatusOK, groupsResponse{UserGroup: g, MemberIDs: memberIDStrings(members)})
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req updateGroupRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	g, err := s.Update(r.Context(), id, UpdateParams(req))
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, g)
}

func (s *Service) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := s.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// memberIDs lists the group's member user IDs.
func (s *Service) memberIDs(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	members, err := s.MemberIDs(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, memberIDStrings(members))
}

// setMembers replaces the group membership.
func (s *Service) setMembers(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req setMembersRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	members, err := parseMemberIDs(req.UserIDs)
	if err != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, err.Error())
		return
	}

	if err := s.SetMembers(r.Context(), id, members); err != nil {
		if errors.Is(err, ErrInvalidIDs) || errors.Is(err, ErrNotFound) {
			responder.Fail(w, r, http.StatusUnprocessableEntity, ErrInvalidIDs.Error())
			return
		}
		writeError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"member_ids": req.UserIDs})
}

// setAllowedClients replaces the group's OIDC client allowlist
// and echoes the stored list back.
func (s *Service) setAllowedClients(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[UserGroupID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req setAllowedClientsRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	if slices.Contains(req.ClientIDs, "") {
		responder.Fail(w, r, http.StatusUnprocessableEntity, ErrInvalidIDs.Error())
		return
	}

	if replaceErr := s.ReplaceAllowedClients(r.Context(), id, req.ClientIDs); replaceErr != nil {
		if errors.Is(replaceErr, ErrInvalidIDs) || errors.Is(replaceErr, ErrNotFound) {
			responder.Fail(w, r, http.StatusUnprocessableEntity, ErrInvalidIDs.Error())
			return
		}
		writeError(w, r, replaceErr)
		return
	}

	clients, err := s.AllowedClientIDs(r.Context(), id)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"oidc_client_ids": clients})
}

// groupsForUser lists the groups a user belongs to.
func (s *Service) groupsForUser(w http.ResponseWriter, r *http.Request) {
	userID, err := identity.ParseID[user.UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	groups, err := s.GroupsForUser(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, groups)
}

// replaceUserGroups replaces the groups a user belongs to.
func (s *Service) replaceUserGroups(w http.ResponseWriter, r *http.Request) {
	userID, err := identity.ParseID[user.UserID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req setUserGroupsRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	groups, parseErr := parseGroupIDs(req.GroupIDs)
	if parseErr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, ErrInvalidIDs.Error())
		return
	}

	if setErr := s.store.ReplaceGroupsForUser(r.Context(), userID, groups); setErr != nil {
		if errors.Is(setErr, ErrInvalidIDs) || errors.Is(setErr, ErrNotFound) {
			responder.Fail(w, r, http.StatusUnprocessableEntity, ErrInvalidIDs.Error())
			return
		}
		writeError(w, r, setErr)
		return
	}

	updated, err := s.GroupsForUser(r.Context(), userID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, updated)
}

// parseGroupIDs converts wire strings to typed IDs.
func parseGroupIDs(raw []string) ([]UserGroupID, error) {
	out := make([]UserGroupID, 0, len(raw))
	for _, text := range raw {
		id, err := identity.ParseID[UserGroupID](text)
		if err != nil {
			return nil, ErrInvalidIDs
		}
		out = append(out, id)
	}
	return out, nil
}

// parseMemberIDs converts wire strings to typed IDs.
func parseMemberIDs(raw []string) ([]user.UserID, error) {
	out := make([]user.UserID, 0, len(raw))
	for _, text := range raw {
		id, err := identity.ParseID[user.UserID](text)
		if err != nil {
			return nil, ErrInvalidIDs
		}
		out = append(out, id)
	}
	return out, nil
}

func memberIDStrings(ids []user.UserID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
