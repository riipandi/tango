package transport

import (
	"context"
	"errors"
	"maps"
	"slices"
	"time"

	"connectrpc.com/connect"

	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/internal/health"
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
		// The correlation id travels in the error's metadata, set for every
		// procedure by the transport's interceptor — the one place both
		// transports can always name the request.
		return nil, connect.NewError(connect.CodeUnavailable, errors.New(health.Message(result)))
	}

	return connect.NewResponse(&systemv1.CheckResponse{
		Status:  string(result.Status),
		Details: rpcCheckDetails(result),
		TookMs:  milliseconds(result.Duration),
	}), nil
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
