package observer_test

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/observer"
)

// grpcCollector is a stand-in for a collector's gRPC listener. It records the
// metadata and the trace service request of every export, so a test can assert
// both that a span arrived and what the exporter sent with it.
//
// The protocol is a real gRPC service rather than a stub HTTP handler, because
// the thing under test is the exporter's own client: a fake that answered HTTP
// would not exercise the branch the configuration selects.
type grpcCollector struct {
	collectortrace.UnimplementedTraceServiceServer

	mu       sync.Mutex
	metadata []metadata.MD
	requests []*collectortrace.ExportTraceServiceRequest
}

func (c *grpcCollector) Export(ctx context.Context, req *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	c.mu.Lock()
	c.metadata = append(c.metadata, md)
	c.requests = append(c.requests, req)
	c.mu.Unlock()

	// An empty response is a successful export; the exporter reads the absence
	// of a partial-success field as full acceptance.
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func (c *grpcCollector) recorded() ([]metadata.MD, []*collectortrace.ExportTraceServiceRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]metadata.MD{}, c.metadata...), append([]*collectortrace.ExportTraceServiceRequest{}, c.requests...)
}

// newGRPCCollector starts a gRPC listener on a loopback port and returns its
// host:port, which is the form a gRPC exporter is addressed by.
func newGRPCCollector(t *testing.T) (string, *grpcCollector) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	collector := &grpcCollector{}
	server := grpc.NewServer()
	collectortrace.RegisterTraceServiceServer(server, collector)

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return listener.Addr().String(), collector
}

// grpcTraceConfig returns a tracing configuration addressed the way gRPC is.
func grpcTraceConfig(target string) config.Config {
	cfg := config.Default()
	cfg.OTEL.Endpoint = target
	cfg.OTEL.Protocol = config.OTELProtocolGRPC
	cfg.OTEL.Tracing.Enable = true
	cfg.OTEL.Tracing.BatchTimeout = 10 * time.Millisecond
	cfg.OTEL.Tracing.ExportTimeout = 2 * time.Second
	return cfg
}

func TestGRPCShipsASpanToARealListener(t *testing.T) {
	// The gRPC branch is a separate exporter package with its own options, so it
	// is proved against a real gRPC service rather than inferred from the HTTP
	// one working. The address is host:port, and the endpoint's URL is reduced
	// to it (see CollectorEndpoint).
	target, collector := newGRPCCollector(t)
	cfg := grpcTraceConfig(target)

	obs, err := observer.New(context.Background(), cfg)
	require.NoError(t, err)

	_, span := obs.Tracer().Tracer("test").Start(context.Background(), "work")
	span.End()
	require.NoError(t, obs.Shutdown(context.Background()))

	_, requests := collector.recorded()
	require.NotEmpty(t, requests, "a span must reach the gRPC listener")

	spans := requests[0].GetResourceSpans()
	require.NotEmpty(t, spans)
	require.NotEmpty(t, spans[0].GetScopeSpans())
	require.NotEmpty(t, spans[0].GetScopeSpans()[0].GetSpans())
	assert.Equal(t, "work", spans[0].GetScopeSpans()[0].GetSpans()[0].GetName())
}

func TestGRPCShipsTheConfiguredHeaders(t *testing.T) {
	// Headers are what a collector authenticating the sender reads, so the value
	// from the configuration must be the one on the wire — and a header the
	// environment set must not be, which is the same rule the HTTP path follows.
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "authorization=Bearer leaked")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS", "x-leaked=from-the-shell")

	target, collector := newGRPCCollector(t)
	cfg := grpcTraceConfig(target)
	cfg.OTEL.Headers = map[string]string{"authorization": "Bearer configured"}

	obs, err := observer.New(context.Background(), cfg)
	require.NoError(t, err)

	_, span := obs.Tracer().Tracer("test").Start(context.Background(), "work")
	span.End()
	require.NoError(t, obs.Shutdown(context.Background()))

	metas, requests := collector.recorded()
	require.NotEmpty(t, requests)

	// gRPC lowercases header names on the wire, so the assertion is on the
	// values rather than on the exact key spelling.
	require.NotEmpty(t, metas)
	assert.Contains(t, metas[0].Get("authorization"), "Bearer configured")
	assert.Empty(t, metas[0].Get("x-leaked"),
		"a header from the environment must not be sent")
}

func TestGRPCIsAddressedByHostAndPort(t *testing.T) {
	// A gRPC exporter wants host:port, so an endpoint written as a URL is
	// reduced to its host and port. The scheme still decides TLS, which is why
	// the URL is not simply rejected.
	cfg := config.Default()
	cfg.OTEL.Protocol = config.OTELProtocolGRPC
	cfg.OTEL.Endpoint = "http://collector.example.com:4317"

	assert.Equal(t, "collector.example.com:4317", cfg.CollectorEndpoint())
	assert.False(t, cfg.CollectorSecure(), "an http scheme is a plaintext connection")

	cfg.OTEL.Endpoint = "https://collector.example.com:4317"
	assert.Equal(t, "collector.example.com:4317", cfg.CollectorEndpoint())
	assert.True(t, cfg.CollectorSecure(), "an https scheme is a TLS connection")
}
