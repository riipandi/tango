package transport

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"resty.dev/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/pkg/responder"
)

// HealthCheckHandler reports service health, probing an upstream endpoint.
// The config is injected via closure instead of the package global.
func HealthCheckHandler(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uaString := fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +%s)", config.AppName, config.AppVersion, cfg.Public.BaseURL)
		httpClient := resty.New().SetHeader("User-Agent", uaString)
		defer httpClient.Close()

		resp, err := httpClient.R().
			SetContext(r.Context()).
			SetTimeout(5 * time.Second).
			Get(cfg.Public.HealthcheckURL)

		if err != nil {
			responder.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unhealthy",
				"error":  err.Error(),
			})
			return
		}

		if !resp.IsSuccess() {
			responder.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":          "unhealthy",
				"error":           "upstream returned non-success status",
				"upstream_status": fmt.Sprintf("%d", resp.StatusCode()),
			})
			return
		}

		responder.WriteJSON(w, http.StatusOK, map[string]string{
			"status":     "healthy",
			"ip_address": resp.String(),
		})
	}
}

func StaticAssetsHandler(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"path": path,
	})
}
