package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/web"
)

type HTTPServer struct {
	Router chi.Router
	Server *http.Server
}

// NewHTTPServer wires middleware, core routes, then registry modules.
func NewHTTPServer(registry *kernel.Registry, cfg *config.Config, log logger.Logger) *HTTPServer {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.JSONRecoverer)
	r.Use(middleware.CORS())

	r.Get("/static/*", StaticAssetsHandler)
	r.Get("/healthz", RootHealthzHandler)
	r.Get("/.well-known/version", VersionHandler)

	registry.Apply(r)

	// Shared /api group; modules register inside it.
	r.Route("/api", func(r chi.Router) {
		r.Get("/", APIRootHandler)
		r.Get("/healthz", HealthCheckHandler)
		r.Get("/version/current", VersionCurrentHandler)
		r.Get("/version/latest", VersionLatestHandler)
		registry.ApplyAPI(r)
	})

	// SPA fallback last; owns root 404.
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
		// No ReadTimeout/WriteTimeout: they would kill streamed
		// bodies and SSE responses; per-request deadlines come from
		// route-specific middleware instead.
	}

	return s.Server.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s.Server == nil {
		return nil
	}
	return s.Server.Shutdown(ctx)
}
