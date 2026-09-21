package logger_test

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collectorlogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/logger"
)

// grpcLogCollector is a stand-in for a collector's gRPC listener. It records the
// metadata and the export request of every call, so a test can assert both that
// a record arrived and what the exporter sent with it.
//
// The protocol is a real gRPC service rather than a stub HTTP handler, because
// the thing under test is the exporter's own client: a fake that answered HTTP
// would not exercise the branch the configuration selects.
type grpcLogCollector struct {
	collectorlogs.UnimplementedLogsServiceServer

	mu       sync.Mutex
	metadata []metadata.MD
	requests []*collectorlogs.ExportLogsServiceRequest
}

func (c *grpcLogCollector) Export(ctx context.Context, req *collectorlogs.ExportLogsServiceRequest) (*collectorlogs.ExportLogsServiceResponse, error) {
	md, _ := metadata.FromIncomingContext(ctx)

	c.mu.Lock()
	c.metadata = append(c.metadata, md)
	c.requests = append(c.requests, req)
	c.mu.Unlock()

	return &collectorlogs.ExportLogsServiceResponse{}, nil
}

func (c *grpcLogCollector) recorded() ([]metadata.MD, []*collectorlogs.ExportLogsServiceRequest) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]metadata.MD{}, c.metadata...), append([]*collectorlogs.ExportLogsServiceRequest{}, c.requests...)
}

// newGRPCLogCollector starts a gRPC listener on a loopback port and returns its
// host:port, which is the form a gRPC exporter is addressed by.
func newGRPCLogCollector(t *testing.T) (string, *grpcLogCollector) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	collector := &grpcLogCollector{}
	server := grpc.NewServer()
	collectorlogs.RegisterLogsServiceServer(server, collector)

	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	return listener.Addr().String(), collector
}

func TestOTLPGRPCShipsToARealListener(t *testing.T) {
	// The gRPC branch is a separate exporter package with its own options, so it
	// is proved against a real gRPC service rather than inferred from the HTTP
	// one working. The address is host:port, and the endpoint's URL is reduced
	// to it (see CollectorEndpoint).
	target, collector := newGRPCLogCollector(t)

	cfg := config.Default()
	cfg.Log.Transport = []string{config.LogTransportOTLP}
	cfg.OTEL.Endpoint = target
	cfg.OTEL.Protocol = config.OTELProtocolGRPC
	cfg.OTEL.Headers = map[string]string{"authorization": "Bearer configured"}

	log, err := logger.New(cfg, logger.WithWriter(&bytes.Buffer{}))
	require.NoError(t, err)

	log.Slog().Info("shipped over gRPC")
	require.NoError(t, log.Shutdown(context.Background()))

	metas, requests := collector.recorded()
	require.NotEmpty(t, requests, "a record must reach the gRPC listener")

	resourceLogs := requests[0].GetResourceLogs()
	require.NotEmpty(t, resourceLogs)
	require.NotEmpty(t, resourceLogs[0].GetScopeLogs())
	require.NotEmpty(t, resourceLogs[0].GetScopeLogs()[0].GetLogRecords())
	assert.Contains(t, resourceLogs[0].GetScopeLogs()[0].GetLogRecords()[0].GetBody().GetStringValue(),
		"shipped over gRPC")

	// gRPC lowercases header names on the wire, so the assertion is on the
	// value rather than on the exact key spelling.
	require.NotEmpty(t, metas)
	assert.Contains(t, metas[0].Get("authorization"), "Bearer configured")
}

func TestOTLPGRPCEndpointPathIsIgnored(t *testing.T) {
	// A gRPC service is addressed by host and port alone, so a URL endpoint is
	// reduced to that. The reduction is what lets one shared otel.endpoint serve
	// a protocol that carries no path.
	cfg := config.Default()
	cfg.OTEL.Protocol = config.OTELProtocolGRPC
	cfg.OTEL.Endpoint = "http://collector.example.com:4317"

	assert.Equal(t, "collector.example.com:4317", cfg.CollectorEndpoint())
	assert.False(t, cfg.CollectorSecure())
}
