package logger

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"time"

	"go.loglayer.dev/transports/otellog/v3"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"

	"github.com/riipandi/tango/internal/config"
)

// otlpPath is the path the OTLP protocol serves logs on. An endpoint that names
// no path gets it, so the common case of a bare host:port works without the
// caller knowing the protocol.
const otlpPath = "/v1/logs"

// otlpTimeout bounds one export attempt.
const otlpTimeout = 10 * time.Second

// otlpSink is the collector transport and the provider behind it, held together
// because the provider owns the queue that Shutdown has to drain.
type otlpSink struct {
	transport *otellog.Transport
	provider  *sdklog.LoggerProvider
}

// newOTLPSink builds the OpenTelemetry sink the configuration names.
//
// Every exporter setting is passed explicitly, including the ones left at their
// default. The exporter otherwise reads OTEL_EXPORTER_OTLP_* variables for its
// endpoint, headers, compression, and TLS material, which would be a second
// configuration source deciding what a deployment logs and where: a stray export
// in a shell could redirect the logs, inject a header, or trust a certificate
// the config file never mentioned. Passing a value for each closes that door and
// leaves the config file as the only thing that decides.
func newOTLPSink(cfg config.Config) (*otlpSink, error) {
	endpoint, err := url.Parse(cfg.Log.OTLP.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("logger: otlp endpoint: %w", err)
	}

	options := []otlploghttp.Option{
		otlploghttp.WithEndpointURL(cfg.Log.OTLP.Endpoint),
		otlploghttp.WithHeaders(map[string]string{}),
		otlploghttp.WithCompression(otlploghttp.NoCompression),
		otlploghttp.WithTimeout(otlpTimeout),
	}
	if endpoint.Path == "" {
		options = append(options, otlploghttp.WithURLPath(otlpPath))
	}
	// A nil TLS configuration is not the same as leaving the option out: it is
	// what stops the exporter from loading OTEL_EXPORTER_OTLP_CERTIFICATE and
	// friends. An https endpoint gets the floor of TLS 1.2 and the system's root
	// certificates, because the config file names no certificate of its own.
	tlsConfig := (*tls.Config)(nil)
	if endpoint.Scheme == "https" {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	options = append(options, otlploghttp.WithTLSClientConfig(tlsConfig))

	exporter, err := otlploghttp.New(context.Background(), options...)
	if err != nil {
		return nil, fmt.Errorf("logger: otlp exporter: %w", err)
	}

	// The resource is built rather than taken from resource.Default, which reads
	// OTEL_SERVICE_NAME and OTEL_RESOURCE_ATTRIBUTES: the service a record is
	// attributed to is a property of this program, not of the shell that started
	// it. The identifier rather than the display name, because a service name is
	// matched by tooling, not read by a person.
	provider := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(exporter)),
		sdklog.WithResource(resource.NewSchemaless(
			semconv.ServiceName(config.AppIdentifier),
			semconv.ServiceVersion(config.AppVersion),
		)),
	)

	return &otlpSink{
		transport: otellog.New(otellog.Config{
			// The instrumentation scope names the emitting service, so a
			// collector can attribute a record without reading its body.
			Name:           config.AppIdentifier,
			Version:        config.AppVersion,
			LoggerProvider: provider,
		}),
		provider: provider,
	}, nil
}

// shutdown drains the queued records and stops the exporter.
func (s *otlpSink) shutdown(ctx context.Context) error {
	if err := s.provider.Shutdown(ctx); err != nil {
		return fmt.Errorf("logger: otlp shutdown: %w", err)
	}
	return nil
}
