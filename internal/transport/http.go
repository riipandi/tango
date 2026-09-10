package transport

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/riipandi/tango/internal/transport/handler"
	"github.com/riipandi/tango/internal/transport/middleware"
	"github.com/riipandi/tango/internal/transport/routes"
	"github.com/riipandi/tango/web"
)

type HTTPServer struct {
	Router chi.Router
	Server *http.Server
}

func NewHTTPServer() *HTTPServer {
	r := chi.NewRouter()

	r.Use(middleware.Logger())
	r.Use(middleware.JSONRecoverer)
	r.Use(middleware.CORS())

	// Well Known: Discovery endpoints for OpenID Connect, etc.
	r.Get("/.well-known/jwks.json", handler.NotImplementedHandler) // Get JSON Web Key Set (JWKS)
	r.Get("/.well-known/version", handler.NotImplementedHandler)   // Get current application version

	r.Route("/api", routes.RegisterAPI)
	r.Get("/static/*", handler.StaticAssetsHandler)

	// Render frontend SPA (must be last)
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
