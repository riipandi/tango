package apiaccess

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// createAPIRequest is the POST /apis payload.
type createAPIRequest struct {
	Name     string `json:"name"`
	Resource string `json:"resource"`
}

func (r createAPIRequest) Validate() error {
	p := CreateParams(r)
	return p.Validate()
}

// updateAPIRequest is the PUT /apis/{id} payload.
type updateAPIRequest struct {
	Name string `json:"name"`
}

func (r updateAPIRequest) Validate() error {
	p := UpdateParams(r)
	return p.Validate()
}

// permissionsRequest is the PUT /apis/{id}/permissions payload: the
// complete permission list (POST semantics).
type permissionsRequest struct {
	Permissions []permissionInput `json:"permissions"`
}

type permissionInput struct {
	Key         string  `json:"key"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitzero"`
}

// grantRequest is the PUT /apis/{id}/clients/{clientId} payload.
type grantRequest struct {
	UserDelegatedAccess        bool     `json:"user_delegated_access"`
	UserDelegatedPermissionIDs []string `json:"user_delegated_permission_ids"`
	ClientAccess               bool     `json:"client_access"`
	ClientPermissionIDs        []string `json:"client_permission_ids"`
}

// cimdAccessRequest is the PUT /apis/{id}/cimd-access payload.
type cimdAccessRequest struct {
	Enabled       bool     `json:"enabled"`
	PermissionIDs []string `json:"permission_ids"`
}

// grantResponse is the apiClientGrantDto wire shape.
type grantResponse struct {
	ClientAccess               bool     `json:"client_access"`
	ClientPermissionIDs        []string `json:"client_permission_ids"`
	UserDelegatedAccess        bool     `json:"user_delegated_access"`
	UserDelegatedPermissionIDs []string `json:"user_delegated_permission_ids"`
}

// clientAPIGrantResponse is the clientApiGrantDto wire shape: one
// grant plus the API it belongs to.
type clientAPIGrantResponse struct {
	API                        API      `json:"api"`
	ClientAccess               bool     `json:"client_access"`
	ClientPermissionIDs        []string `json:"client_permission_ids"`
	UserDelegatedAccess        bool     `json:"user_delegated_access"`
	UserDelegatedPermissionIDs []string `json:"user_delegated_permission_ids"`
	CIMDGrantedAccess          bool     `json:"cimd_granted_access"`
	CIMDGrantedPermissionIDs   []string `json:"cimd_granted_permission_ids"`
}

// APIRoutes mounts the API registry endpoints inside the shared
// /api group, behind the admin guard when one is wired.
func (s *Service) APIRoutes(r chi.Router) {
	mount := func(ar chi.Router) {
		ar.Get("/apis", s.list)
		ar.Post("/apis", s.create)
		ar.Route("/apis/{id}", func(api chi.Router) {
			api.Get("/", s.get)
			api.Put("/", s.update)
			api.Delete("/", s.deleteAPI)
			api.Get("/assignable-clients", s.assignableClients)
			api.Put("/cimd-access", s.setCIMDAccess)
			api.Get("/clients", s.clientsWithAccess)
			api.Put("/clients/{clientId}", s.upsertGrant)
			api.Delete("/clients/{clientId}", s.deleteGrant)
			api.Put("/permissions", s.setPermissions)
		})
		ar.Get("/api-access/{clientId}/apis", s.apisForClient)
		ar.Get("/api-access/{clientId}/assignable-apis", s.assignableAPIs)
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
	var req createAPIRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	a, err := s.Create(r.Context(), CreateParams(req))
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, a)
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	apis, total, err := s.List(r.Context(), ListParams{
		Query:            r.URL.Query().Get("search"),
		PaginationParams: params,
	})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, apis, responder.WithPaginationFrom(params, total))
}

func (s *Service) get(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	a, err := s.GetByID(r.Context(), id)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, a)
}

func (s *Service) update(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req updateAPIRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	a, err := s.Update(r.Context(), id, UpdateParams(req))
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, a)
}

func (s *Service) deleteAPI(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := s.Delete(r.Context(), id); err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// setPermissions list-replaces the API's permissions.
func (s *Service) setPermissions(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req permissionsRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	perms := make([]PermissionInput, 0, len(req.Permissions))
	for _, p := range req.Permissions {
		perms = append(perms, PermissionInput(p))
	}

	out, err := s.SetPermissions(r.Context(), id, perms)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"permissions": out})
}

// clientsWithAccess pages the clients holding a grant.
func (s *Service) clientsWithAccess(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	s.listClients(w, r, id, true)
}

// assignableClients pages the clients without a grant.
func (s *Service) assignableClients(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	s.listClients(w, r, id, false)
}

func (s *Service) listClients(w http.ResponseWriter, r *http.Request, id APIID, granted bool) {
	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	listParams := ListParams{Query: r.URL.Query().Get("search"), PaginationParams: params}
	var (
		clients []ClientRef
		total   int
	)
	if granted {
		clients, total, err = s.ClientsWithAccess(r.Context(), id, listParams)
	} else {
		clients, total, err = s.AssignableClients(r.Context(), id, listParams)
	}
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, clients, responder.WithPaginationFrom(params, total))
}

// upsertGrant writes one client's grant on the API.
func (s *Service) upsertGrant(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}
	clientID := chi.URLParam(r, "clientId")

	var req grantRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	g, err := s.UpsertGrant(r.Context(), id, clientID, GrantParams(req))
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, grantResponse{
		ClientAccess:               g.ClientAccess,
		ClientPermissionIDs:        g.ClientPermissionIDs,
		UserDelegatedAccess:        g.UserDelegatedAccess,
		UserDelegatedPermissionIDs: g.UserDelegatedPermissionIDs,
	})
}

// deleteGrant revokes one client's grant on the API.
func (s *Service) deleteGrant(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	if err := s.DeleteGrant(r.Context(), id, chi.URLParam(r, "clientId")); err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, map[string]any{"deleted": true})
}

// apisForClient lists the APIs a client may access, with the access
// split per subject.
func (s *Service) apisForClient(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")

	grants, err := s.GrantsForClient(r.Context(), clientID)
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}

	out := make([]clientAPIGrantResponse, 0, len(grants))
	for _, g := range grants {
		apiID, parseErr := identity.ParseID[APIID](g.APIID)
		if parseErr != nil {
			continue
		}
		a, getErr := s.GetByID(r.Context(), apiID)
		if getErr != nil {
			continue
		}
		cimdAccess, cimdPerms := cimdAccessFor(a, g)
		out = append(out, clientAPIGrantResponse{
			API:                        a,
			ClientAccess:               g.ClientAccess,
			ClientPermissionIDs:        g.ClientPermissionIDs,
			UserDelegatedAccess:        g.UserDelegatedAccess,
			UserDelegatedPermissionIDs: g.UserDelegatedPermissionIDs,
			CIMDGrantedAccess:          cimdAccess,
			CIMDGrantedPermissionIDs:   cimdPerms,
		})
	}
	responder.Success(w, r, http.StatusOK, out)
}

// assignableAPIs pages the APIs a client may still be granted.
func (s *Service) assignableAPIs(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "clientId")

	params, err := responder.ParsePagination(r)
	if err != nil {
		responder.BadRequestJSON(w, r, responder.ErrInvalidPagination.Error())
		return
	}

	apis, total, err := s.AssignableAPIs(r.Context(), clientID, ListParams{
		Query:            r.URL.Query().Get("search"),
		PaginationParams: params,
	})
	if err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
		return
	}
	responder.Success(w, r, http.StatusOK, apis, responder.WithPaginationFrom(params, total))
}

// setCIMDAccess toggles the API-level CIMD flag plus allowlist.
func (s *Service) setCIMDAccess(w http.ResponseWriter, r *http.Request) {
	id, err := identity.ParseID[APIID](chi.URLParam(r, "id"))
	if err != nil {
		responder.NotFoundJSON(w, r)
		return
	}

	var req cimdAccessRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}

	a, err := s.SetCIMDAccess(r.Context(), id, req.Enabled, req.PermissionIDs)
	if err != nil {
		responder.WriteError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, a)
}

// cimdAccessFor computes the CIMD access split for one grant: the
// API flag gates access; the allowlist gates each permission.
func cimdAccessFor(a API, g Grant) (bool, []string) {
	if !a.AllowCIMDClients {
		return false, []string{}
	}
	allowed := map[string]bool{}
	for _, p := range a.Permissions {
		if p.AllowedForCIMDClients {
			allowed[p.ID] = true
		}
	}
	ids := make([]string, 0, len(g.UserDelegatedPermissionIDs))
	for _, pid := range g.UserDelegatedPermissionIDs {
		if allowed[pid] {
			ids = append(ids, pid)
		}
	}
	return true, ids
}
