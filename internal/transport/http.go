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

// RPC deadline budget: every RPC request gets a hard context
// deadline so a stalled handler cannot pin a connection.
const rpcRequestTimeout = 30 * time.Second

// RouteSet carries the explicit route-mount callbacks from the
// application runtime so the transport boundary needs no registry.
type RouteSet struct {
	MountRoot func(chi.Router)
	MountAPI  func(chi.Router)
	// RequireSession protects metadata endpoints that upstream serves
	// to any signed-in user; nil leaves those routes unmounted.
	RequireSession kernel.Guard
}

// NewHTTPServer wires middleware, core routes, and runtime routes.
// The checks back the API readiness endpoint; each check is owned by
// the composition root.
func NewHTTPServer(routes RouteSet, cfg *config.Config, log logger.Logger, limiter func(http.Handler) http.Handler, latest LatestVersionSource, checks []HealthCheck) *HTTPServer {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.RequestLogger(log))
	r.Use(middleware.JSONRecoverer)
	r.Use(middleware.CORS())

	r.Get("/static/*", StaticAssetsHandler)
	// Root healthz is liveness: the process is up, dependencies are
	// not touched so a broken database cannot restart the pod loop.
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
		r.Get("/healthz", newHealthHandler(checks).ServeHTTP)
		if routes.RequireSession != nil {
			r.Get("/version/current", routes.RequireSession(http.HandlerFunc(VersionCurrentHandler)).ServeHTTP)
		}
		r.Get("/version/latest", VersionLatestHandler(latest))
		if routes.MountAPI != nil {
			routes.MountAPI(r)
		}
	})

	// Mount the ConnectRPC surface. Shares the global request-ID,
	// logger, recovery, and CORS middleware; adds a per-request
	// deadline. The mount happens before the SPA fallback so unknown
	// /rpc paths answer Connect 404s, never the SPA document. chi's
	// Mount only shifts its route context, never r.URL.Path, so the
	// Connect mux needs an explicit StripPrefix.
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequestTimeout(rpcRequestTimeout))
		r.Mount("/rpc", http.StripPrefix("/rpc", rpcHandler()))
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
	}

	return s.Server.ListenAndServe()
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s.Server == nil {
		return nil
	}
	return s.Server.Shutdown(ctx)
}
