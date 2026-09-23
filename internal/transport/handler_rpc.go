package transport

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/pkg/responder"
)

// rpcHealthService answers the readiness procedure over ConnectRPC.
//
// It publishes what the REST endpoint publishes, from the same checker, so the
// two surfaces cannot drift: one check set, one cache, one aggregate. The
// routing that reaches it lives in rpc.go.
type rpcHealthService struct {
	checker *health.Checker
}

func newRPCHealthService(checker *health.Checker) *rpcHealthService {
	return &rpcHealthService{checker: checker}
}

// Check runs the checks and answers with the readiness document.
//
// An unhealthy system fails with `unavailable`, which the transport answers as
// 503 — the status a probe acts on — and the message names the checks that are
// down, because a bare status does not say what to look at.
func (s *rpcHealthService) Check(ctx context.Context, _ *connect.Request[systemv1.CheckRequest]) (*connect.Response[systemv1.CheckResponse], error) {
	// A router built without a checker is a wiring defect; the registry builds
	// the two together, so a working composition cannot reach this.
	if s.checker == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("health check unavailable"))
	}

	result := s.checker.Check(ctx)
	if !result.Healthy() {
		err := connect.NewError(connect.CodeUnavailable, errors.New(health.Message(result)))
		// A failed call carries no metadata block, so the correlation id
		// travels in the header instead — the one place both transports can
		// always name the request.
		if id := responder.RequestIDFromContext(ctx); id != "" {
			err.Meta().Set(responder.RequestIDHeader, id)
		}
		return nil, err
	}

	return connect.NewResponse(&systemv1.CheckResponse{
		Metadata: rpcMetadata(ctx, http.StatusOK),
		Status:   string(result.Status),
		Details:  rpcCheckDetails(result),
		TookMs:   milliseconds(result.Duration),
	}), nil
}

// rpcMetadata builds the shared metadata block for one response. It writes
// what the REST envelope writes for the same request, so a client reads one
// status code and one correlation id from either transport.
func rpcMetadata(ctx context.Context, statusCode int32) *commonv1.ResponseMetadata {
	meta := &commonv1.ResponseMetadata{StatusCode: proto.Int32(statusCode)}
	if id := responder.RequestIDFromContext(ctx); id != "" {
		meta.RequestId = new(id)
	}
	return meta
}

// rpcCheckDetails renders the per-check results in check-name order, which is
// the order the REST body writes them in.
func rpcCheckDetails(result health.Result) []*systemv1.CheckDetail {
	names := slices.Sorted(maps.Keys(result.Details))
	details := make([]*systemv1.CheckDetail, 0, len(names))
	for _, name := range names {
		detail := result.Details[name]
		details = append(details, &systemv1.CheckDetail{
			Name:     detail.Name,
			Status:   string(detail.Status),
			Target:   detail.Target,
			Error:    detail.Error,
			Optional: detail.Optional,
			TookMs:   milliseconds(detail.Duration),
		})
	}
	return details
}

// milliseconds renders a duration as fractional milliseconds, the unit both
// transports publish.
func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
