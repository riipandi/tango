package scimsync

// APIFeature mounts the provider surface. The REST routes are gone:
// the whole surface serves ConnectRPC exclusively (see handler_rpc.go).
type APIFeature struct {
	service *Service
}

// New builds the feature over the sync service.
func New(service *Service) *APIFeature { return &APIFeature{service: service} }

// Name implements federation.Feature.
func (*APIFeature) Name() string { return "scimsync" }
