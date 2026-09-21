package transport

import (
	"fmt"
	"net/http"

	"github.com/riipandi/tango/internal/config"
)

// NewServer builds the HTTP server the router is served through, with the
// timeouts the configuration holds. The caller owns the listener and the
// graceful drain; the server here is only the bound handler and its timeouts.
func NewServer(cfg config.Config, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           handler,
		ReadTimeout:       cfg.Server.ReadTimeout,
		ReadHeaderTimeout: cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}
}
