package launcher

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/registry"
	"github.com/riipandi/tango/internal/transport"
	"github.com/spf13/cobra"
)

var serveHost string
var servePort string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the application server",
	Run: func(cmd *cobra.Command, args []string) {
		config.ApplyFlags(serveHost, servePort)

		cfg, err := config.Load()
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}

		reg := registry.New(registry.Deps{Config: cfg})
		if err := reg.Start(cmd.Context()); err != nil {
			log.Fatalf("failed to start modules: %v", err)
		}

		srv := transport.NewHTTPServer(reg, cfg)
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

		go func() {
			log.Printf("listening on http://%s\n", addr)
			if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		// signal.NotifyContext (Go 1.26+): the returned context is
		// canceled with the received signal as its cause, so the
		// shutdown path can report exactly which signal arrived.
		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		<-ctx.Done()
		log.Printf("received shutdown signal: %v", context.Cause(ctx))

		log.Println("shutting down server...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown error: %v", err)
		}

		if err := reg.Stop(shutdownCtx); err != nil {
			log.Printf("module shutdown errors: %v", err)
		}

		log.Println("server stopped")
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Host to bind to")
	serveCmd.Flags().StringVar(&servePort, "port", "", "Port to bind to")
}
