package middleware

import (
	"net/http"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/riipandi/tango/internal/logger"
	"go.loglayer.dev/v3"
)

// RequestLogger logs one entry per request (5xx error, 4xx warning).
func RequestLogger(log logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)

			next.ServeHTTP(ww, r)

			entry := log.WithMetadata(loglayer.M{
				"method":      r.Method,
				"path":        r.URL.Path,
				"status":      ww.Status(),
				"bytes":       ww.BytesWritten(),
				"duration_ms": time.Since(start).Milliseconds(),
			})
			switch {
			case ww.Status() >= http.StatusInternalServerError:
				entry.Error("request failed")
			case ww.Status() >= http.StatusBadRequest:
				entry.Warn("request rejected")
			default:
				entry.Info("request served")
			}
		})
	}
}
