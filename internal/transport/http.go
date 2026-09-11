package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/transport/handler"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/web"
)

type HTTPServer struct {
	Router chi.Router
	Server *http.Server
}

// NewHTTPServer assembles the application: shared middleware, core
// routes, then every module from the registry mounts itself.
func NewHTTPServer(registry *kernel.Registry) *HTTPServer {
	r := chi.NewRouter()

	r.Use(middleware.Logger())
	r.Use(middleware.JSONRecoverer)
	r.Use(middleware.CORS())

	// Core endpoints.
	r.Get("/healthz", handler.HealthzHandler)
	r.Get("/static/*", handler.StaticAssetsHandler)

	// Modules mount root-level routes (wellknown, ...).
	registry.Apply(r)

	// Shared /api group; APIRoutable modules register inside it.
	r.Route("/api", registry.ApplyAPI)

	// Render frontend SPA (must be last); web.SetupStatic also owns
	// the root 404/SPA fallback.
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
	}

	return s.Server.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	return s.Server.Shutdown(ctx)
}
