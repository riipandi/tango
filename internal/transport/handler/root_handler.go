package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"resty.dev/v3"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/transport/responder"
)

func NotImplementedHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "Not yet implemented",
	})
}

func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	uaString := fmt.Sprintf("Mozilla/5.0 (compatible; %s/%s; +%s)", config.AppName, config.AppVersion, config.C.Public.BaseURL)
	httpClient := resty.New().SetHeader("User-Agent", uaString)
	defer httpClient.Close()

	resp, err := httpClient.R().
		SetContext(r.Context()).
		SetTimeout(5 * time.Second).
		Get("https://api.ipify.org")

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

	ipAddress := resp.String()

	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"status":     "healthy",
		"ip_address": ipAddress,
	})
}

func APIRootHandler(w http.ResponseWriter, r *http.Request) {
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"name":     config.AppName,
		"version":  config.AppVersion,
		"platform": config.Platform,
		"build":    config.BuildDate,
		"hash":     config.BuildHash,
	})
}

func StaticAssetsHandler(w http.ResponseWriter, r *http.Request) {
	path := chi.URLParam(r, "*")
	responder.WriteJSON(w, http.StatusOK, map[string]string{
		"path": path,
	})
}
