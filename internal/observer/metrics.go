package observer

import (
	"crypto/tls"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"

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

// grpcCompressor maps the configured name to what a gRPC exporter accepts.
//
// gRPC supports gzip alone, so `none` and gzip are the two values and the
// exporter treats anything else as no compression while reporting it through
// otel.Handle. Validate has already refused a third value by the time this runs.
func grpcCompressor(cfg config.Config) string {
	if cfg.OTEL.Compression == config.OTELCompressionNone {
		return ""
	}
	return "gzip"
}

// grpcTransport returns the transport credentials for a gRPC exporter.
//
// Explicit credentials are what stop the exporter from applying the TLS
// material OTEL_EXPORTER_OTLP_CERTIFICATE and its friends describe: those
// options are appended before a caller's, and transport credentials take
// priority over both the insecure and the TLS default, so passing them closes
// the door at the point where the exporter opens it.
func grpcTransport(secure bool) credentials.TransportCredentials {
	if secure {
		return credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	}
	return insecure.NewCredentials()
}

// tlsConfig returns the TLS configuration for an HTTP collector endpoint.
//
// A nil configuration is not the same as leaving the option out: passing nil is
// what stops the exporter from loading OTEL_EXPORTER_OTLP_CERTIFICATE and
// friends, which would be a second source deciding what this process trusts. A
// secure endpoint gets the floor of TLS 1.2 and the system's root certificates,
// because the configuration names no certificate of its own.
func tlsConfig(secure bool) *tls.Config {
	if secure {
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
