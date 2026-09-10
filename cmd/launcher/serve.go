package launcher

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/transport"
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

		srv := transport.NewHTTPServer()
		addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

		go func() {
			log.Printf("listening on http://%s\n", addr)
			if err := srv.ListenAndServe(addr); err != nil && err != http.ErrServerClosed {
				log.Fatalf("server error: %v", err)
			}
		}()

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		<-quit

		log.Println("shutting down server...")

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("shutdown error: %v", err)
		}

		log.Println("server stopped")
	},
}

func init() {
	serveCmd.Flags().StringVar(&serveHost, "host", "", "Host to bind to")
	serveCmd.Flags().StringVar(&servePort, "port", "", "Port to bind to")
}
