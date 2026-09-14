package scimsync

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/pkg/responder"
	"github.com/riipandi/tango/pkg/validate"
)

// APIFeature mounts the service-provider endpoints under /api.
type APIFeature struct {
	service *Service
}

// New builds the feature over the sync service.
func New(service *Service) *APIFeature { return &APIFeature{service: service} }

// Name implements federation.Feature.
func (*APIFeature) Name() string { return "scimsync" }

// APIRoutes mounts the SCIM service-provider endpoints.
func (f *APIFeature) APIRoutes(r chi.Router) {
	r.Get("/oidc/clients/{id}/scim-service-provider", f.getByClient)
	r.Post("/scim/service-provider", f.create)
	r.Post("/scim/service-provider/{id}/sync", f.sync)
	r.Put("/scim/service-provider/{id}", f.update)
	r.Delete("/scim/service-provider/{id}", f.delete)
}

// parseID converts a URL param into the typed ID.
func parseID(raw string) (SCIMServiceProviderID, error) {
	return typeid.Parse[SCIMServiceProviderID](raw)
}

// providerResponse is the wire shape; the token is only present on
// write responses (upstream shows it once).
type providerResponse struct {
	ID           string     `json:"id"`
	Endpoint     string     `json:"endpoint"`
	Token        string     `json:"token,omitempty"`
	OIDCClientID string     `json:"oidc_client_id"`
	LastSyncedAt *time.Time `json:"last_synced_at,omitzero"`
	CreatedAt    time.Time  `json:"created_at"`
}

func toResponse(p ServiceProvider, withToken bool) providerResponse {
	resp := providerResponse{
		ID:           p.ID.String(),
		Endpoint:     p.Endpoint,
		OIDCClientID: p.OIDCClientID,
		LastSyncedAt: p.LastSyncedAt,
		CreatedAt:    p.CreatedAt,
	}
	if withToken {
		resp.Token = p.Token
	}
	return resp
}

type createRequest struct {
	Endpoint     string `json:"endpoint"`
	Token        string `json:"token"`
	OIDCClientID string `json:"oidc_client_id"`
}

// Validate applies the rules mirroring upstream DTO constraints.
func (r createRequest) Validate() error {
	return UpsertParams(r).Validate()
}

func (f *APIFeature) getByClient(w http.ResponseWriter, r *http.Request) {
	clientID := chi.URLParam(r, "id")
	provider, err := f.service.store.GetByClient(r.Context(), clientID)
	if err != nil {
		respondStoreError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, toResponse(provider, false))
}

func (f *APIFeature) create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	params := UpsertParams(req)
	provider, err := f.service.store.Create(r.Context(), params)
	if err != nil {
		respondStoreError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusCreated, toResponse(provider, true))
}

func (f *APIFeature) update(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if verr := validate.Request(r.Body, &req); verr != nil {
		responder.Fail(w, r, http.StatusUnprocessableEntity, "validation failed",
			responder.WithError(validate.FieldErrors(verr)))
		return
	}
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "service provider not found")
		return
	}
	params := UpsertParams(req)
	provider, err := f.service.store.Update(r.Context(), id, params)
	if err != nil {
		respondStoreError(w, r, err)
		return
	}
	responder.Success(w, r, http.StatusOK, toResponse(provider, true))
}

func (f *APIFeature) delete(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "service provider not found")
		return
	}
	if err := f.service.store.Delete(r.Context(), id); err != nil {
		respondStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (f *APIFeature) sync(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(chi.URLParam(r, "id"))
	if err != nil {
		responder.Fail(w, r, http.StatusNotFound, "service provider not found")
		return
	}
	provider, err := f.service.store.GetByID(r.Context(), id)
	if err != nil {
		respondStoreError(w, r, err)
		return
	}
	if err := f.service.SyncProvider(r.Context(), provider); err != nil {
		responder.Fail(w, r, http.StatusBadGateway, "scim sync failed",
			responder.WithError(err.Error()))
		return
	}
	w.WriteHeader(http.StatusOK)
}

func respondStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		responder.Fail(w, r, http.StatusNotFound, "service provider not found")
	case errors.Is(err, ErrUnknownClient):
		responder.Fail(w, r, http.StatusUnprocessableEntity, "unknown oidc client")
	case errors.Is(err, ErrDuplicate):
		responder.Fail(w, r, http.StatusConflict, "client already has a scim service provider")
	default:
		responder.Fail(w, r, http.StatusInternalServerError, "internal error")
	}
}
