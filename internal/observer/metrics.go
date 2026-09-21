package observer

import (
	"crypto/tls"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"

	"github.com/riipandi/tango/internal/config"
)

// metricCompression maps the configured name to the metric exporter's own value.
//
// It is a separate function from compression because the two OTLP exporters are
// separate packages with separate types, so there is no shared constant to name.
func metricCompression(cfg config.Config) otlpmetrichttp.Compression {
	if cfg.OTEL.Compression == config.OTELCompressionNone {
		return otlpmetrichttp.NoCompression
	}
	return otlpmetrichttp.GzipCompression
}

// tlsConfig returns the TLS configuration for a collector endpoint.
//
// A nil configuration is not the same as leaving the option out: passing nil is
// what stops the exporter from loading OTEL_EXPORTER_OTLP_CERTIFICATE and
// friends, which would be a second source deciding what this process trusts. An
// https endpoint gets the floor of TLS 1.2 and the system's root certificates,
// because the configuration names no certificate of its own.
func tlsConfig(endpoint string) *tls.Config {
	if strings.HasPrefix(endpoint, "https://") {
		return &tls.Config{MinVersion: tls.VersionTLS12}
	}
	return nil
}

// newRegistry returns the registry the Prometheus exposition is served from.
//
// It is a fresh registry rather than the package default, so a scrape reports
// this application's instruments and nothing a dependency registered globally:
// a Go runtime collector, a database driver's pool statistics, or another
// library's own metrics would otherwise appear on this endpoint and be
// attributed to this service. A service that wants the process collectors
// registers them here, deliberately, rather than inheriting them by accident.
func newRegistry() *prometheus.Registry {
	return prometheus.NewRegistry()
}

// MetricsHandler returns the HTTP handler that serves the Prometheus
// exposition, or nil when metrics are switched off.
//
// A nil handler is the honest answer for a disabled signal: a caller mounts it
// only when there is something to serve, rather than exposing an endpoint that
// reports nothing.
func (o *Observer) MetricsHandler() http.Handler {
	if o == nil || o.registry == nil {
		return nil
	}
	return promhttp.HandlerFor(o.registry, promhttp.HandlerOpts{})
}
