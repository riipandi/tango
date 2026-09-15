package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/web"
)

type HTTPServer struct {
	Router chi.Router
	Server *http.Server
}

// RouteSet carries the explicit route-mount callbacks from the
// application runtime so the transport boundary needs no registry.
type RouteSet struct {
	MountRoot func(chi.Router)
	MountAPI  func(chi.Router)
}

// NewHTTPServer wires middleware, core routes, and runtime routes.
func NewHTTPServer(routes RouteSet, cfg *config.Config, log logger.Logger, limiter func(http.Handler) http.Handler, latest LatestVersionSource) *HTTPServer {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.JSONRecoverer)
	r.Use(middleware.CORS())

	r.Get("/static/*", StaticAssetsHandler)
	r.Get("/healthz", RootHealthzHandler)
	r.Get("/.well-known/version", VersionHandler)

	if routes.MountRoot != nil {
		routes.MountRoot(r)
	}

	// Mount the shared /api group.
	r.Route("/api", func(r chi.Router) {
		if limiter != nil {
			r.Use(limiter)
		}
		r.Get("/", APIRootHandler)
		r.Get("/healthz", HealthCheckHandler)
		r.Get("/version/current", VersionCurrentHandler)
		r.Get("/version/latest", VersionLatestHandler(latest))
		if routes.MountAPI != nil {
			routes.MountAPI(r)
		}
	})

	// Mount the SPA fallback last.
	web.SetupStatic(r)

	return &HTTPServer{Router: r}
}

func (s *HTTPServer) ListenAndServe(addr string) error {
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)

	s.Server = &http.Server{
		Addr:              addr,
		Handler:           s.Router,
		Protocols:         protocols,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// Read and write deadlines are set by route-specific middleware.
	}

	return s.Server.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s.Server == nil {
		return nil
	}
	return s.Server.Shutdown(ctx)
}
